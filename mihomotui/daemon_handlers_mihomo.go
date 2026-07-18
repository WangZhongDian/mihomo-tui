package mihomotui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleMihomoAPICredentials 向通过 IPC 授权的本地客户端提供连接 mihomo API 所需的最小凭据。
// 常规 /config 响应始终掩码 secret，避免日志、调试输出和无意的配置同步泄露它。
func (d *Daemon) handleMihomoAPICredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	cfg := GlobalConfig()
	writeJSON(w, http.StatusOK, ok(map[string]string{
		"external_controller": cfg.Mihomo.ExternalController,
		"secret":              cfg.Mihomo.Secret,
	}))
}

func (d *Daemon) handleMihomoStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	running, pid := d.mihomoProcess.Status()
	writeJSON(w, http.StatusOK, ok(MihomoStatusResponse{Running: running, PID: pid}))
}

func (d *Daemon) handleMihomoStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}

	// 先检查 mihomo 配置目录和文件是否存在
	cfg := GlobalConfig()
	mihomoDir := filepath.Dir(cfg.MihomoConfigPath)
	if _, err := os.Stat(mihomoDir); os.IsNotExist(err) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("mihomo 配置目录不存在: %s，请先在订阅页面应用订阅生成配置", mihomoDir))
		return
	}
	configPath := filepath.Join(mihomoDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("mihomo 配置文件不存在: %s，请先在订阅页面应用订阅生成配置", configPath))
		return
	}

	if err := d.mihomoProcess.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("启动失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ok(nil))
}

func (d *Daemon) handleMihomoStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	if err := d.mihomoProcess.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("停止失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ok(nil))
}

func (d *Daemon) handleMihomoRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	if err := d.mihomoProcess.Restart(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("重启失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ok(nil))
}

func (d *Daemon) handleMihomoUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := readJSON(r, &req); err != nil {
		Warnf("解析升级请求失败: %v", err)
	}

	version := req.Version
	if version == "" {
		var err error
		version, err = GetMihomoLastVersion()
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("获取最新版本失败: %w", err))
			return
		}
	}

	d.mu.Lock()
	if d.upgradeProgress.Status == "downloading" || d.upgradeProgress.Status == "extracting" {
		d.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Errorf("已有升级任务在进行中"))
		return
	}
	d.upgradeProgress = UpgradeProgress{Status: "downloading", Percent: 0, Message: "准备下载..."}
	d.mu.Unlock()

	// 异步下载安装
	go func() {
		_, err := DownloadMihomo(version, func(percent int, status string) {
			d.mu.Lock()
			d.upgradeProgress.Percent = percent
			d.upgradeProgress.Status = status
			switch status {
			case "downloading":
				d.upgradeProgress.Message = fmt.Sprintf("正在下载 %d%%", percent)
			case "extracting":
				d.upgradeProgress.Message = "正在解压..."
			case "done":
				d.upgradeProgress.Message = fmt.Sprintf("已更新至 v%s", version)
			case "error":
				d.upgradeProgress.Message = "下载或安装失败"
			}
			d.mu.Unlock()
		})
		d.mu.Lock()
		if err != nil {
			d.upgradeProgress.Status = "error"
			d.upgradeProgress.Message = err.Error()
		} else {
			d.upgradeProgress.Status = "done"
			d.upgradeProgress.Percent = 100
			d.upgradeProgress.Message = fmt.Sprintf("已更新至 v%s", version)
		}
		d.mu.Unlock()
	}()

	writeJSON(w, http.StatusOK, ok(fmt.Sprintf("开始下载 v%s", version)))
}

func (d *Daemon) handleMihomoUpgradeProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	d.mu.RLock()
	progress := d.upgradeProgress
	d.mu.RUnlock()
	// A completed download is atomically renamed to its final version path.
	// Reconcile progress with that durable state so a lost/late callback can
	// never leave the UI forever at 0% while the binary already exists.
	if (progress.Status == "downloading" || progress.Status == "extracting") && progress.Version != "" {
		if info, ok := FindMihomoVersionInfo(progress.Version); ok && info.Downloaded {
			d.mu.Lock()
			if d.upgradeProgress.Version == progress.Version && (d.upgradeProgress.Status == "downloading" || d.upgradeProgress.Status == "extracting") {
				d.upgradeProgress.Status = "done"
				d.upgradeProgress.Percent = 100
				d.upgradeProgress.Message = "已下载 v" + progress.Version
				d.upgradeProgress.DownloadedBytes = info.Size
				d.upgradeProgress.TotalBytes = info.Size
			}
			progress = d.upgradeProgress
			d.mu.Unlock()
		}
	}
	writeJSON(w, http.StatusOK, ok(progress))
}

func (d *Daemon) handleMihomoVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	version, err := GetCurrentMihomoVersion()
	if err != nil {
		writeJSON(w, http.StatusOK, ok(map[string]string{"version": "", "error": err.Error()}))
		return
	}
	writeJSON(w, http.StatusOK, ok(map[string]string{"version": version}))
}

func (d *Daemon) handleMihomoLatestVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	// Legacy endpoint now shares the resilient catalog implementation. It first
	// uses a fresh catalog and falls back to an existing cache when offline.
	if _, err := RefreshMihomoVersionCatalog(); err != nil && len(MihomoVersionList()) == 0 {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	latest, err := latestStableMihomoVersion()
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, ok(map[string]string{"version": latest.Version}))
}

func (d *Daemon) handleExternalResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok(CheckExternalResources()))
}

func (d *Daemon) handleDownloadExternalResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	if err := DownloadExternalResources(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("下载外部资源失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ok(nil))
}

// handleMihomoVersions exposes cached release metadata and local installation state.
func (d *Daemon) handleMihomoVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	checkedAt, source, lastError := MihomoVersionCacheStatus()
	writeJSON(w, http.StatusOK, ok(map[string]any{
		"versions": MihomoVersionList(), "checked_at": checkedAt, "source": source, "last_error": lastError,
	}))
}
func (d *Daemon) handleMihomoVersionsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	versions, err := RefreshMihomoVersionCatalog()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ok(versions))
}
func (d *Daemon) handleMihomoVersionDetail(w http.ResponseWriter, r *http.Request) {
	version := strings.TrimPrefix(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/mihomo/versions/"), "/"), "v")
	if version == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("版本不能为空"))
		return
	}
	switch r.Method {
	case http.MethodPost:
		action := r.URL.Query().Get("action")
		switch action {
		case "download":
			d.startMihomoVersionDownload(w, version)
		case "activate":
			if err := d.activateMihomoVersion(version); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeJSON(w, http.StatusOK, ok(nil))
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("不支持的版本操作"))
		}
	case http.MethodDelete:
		if err := DeleteMihomoVersion(version); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
func (d *Daemon) startMihomoVersionDownload(w http.ResponseWriter, version string) {
	d.mu.Lock()
	if d.upgradeProgress.Status == "downloading" || d.upgradeProgress.Status == "extracting" {
		d.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Errorf("已有内核下载任务在进行中"))
		return
	}
	d.upgradeProgress = UpgradeProgress{Status: "downloading", Percent: 0, Message: "准备下载 v" + version, Version: version}
	d.mu.Unlock()
	go func() {
		_, err := DownloadMihomoVersionWithProgress(version, func(progress DownloadProgress) {
			d.mu.Lock()
			d.upgradeProgress.Percent = progress.Percent
			d.upgradeProgress.Status = progress.Status
			d.upgradeProgress.DownloadedBytes = progress.DownloadedBytes
			d.upgradeProgress.TotalBytes = progress.TotalBytes
			d.upgradeProgress.Version = version
			if progress.Status == "downloading" {
				if progress.TotalBytes > 0 {
					d.upgradeProgress.Message = fmt.Sprintf("正在下载 v%s：%d%%", version, progress.Percent)
				} else {
					d.upgradeProgress.Message = fmt.Sprintf("正在下载 v%s：%s", version, FormatSize(progress.DownloadedBytes))
				}
			} else if progress.Status == "extracting" {
				d.upgradeProgress.Message = "正在解压 v" + version
			}
			d.mu.Unlock()
		})
		d.mu.Lock()
		defer d.mu.Unlock()
		if err != nil {
			d.upgradeProgress.Status = "error"
			d.upgradeProgress.Message = err.Error()
		} else {
			d.upgradeProgress.Status = "done"
			d.upgradeProgress.Percent = 100
			d.upgradeProgress.Message = "已下载 v" + version
		}
	}()
	writeJSON(w, http.StatusAccepted, ok(nil))
}
func (d *Daemon) activateMihomoVersion(version string) error {
	running, _ := d.mihomoProcess.Status()
	old := GlobalConfig()
	oldVersion := old.MihomoActiveVersion
	oldPath := old.MihomoBinaryPath
	if err := ActivateMihomoVersion(version); err != nil {
		return err
	}
	if !running {
		return nil
	}
	restartErr := d.mihomoProcess.Restart()
	if restartErr == nil {
		return nil
	}
	_, rollbackErr := UpdateGlobalConfig(func(c *Config) error { c.MihomoActiveVersion = oldVersion; c.MihomoBinaryPath = oldPath; return nil })
	if rollbackErr == nil {
		_ = d.mihomoProcess.Restart()
	}
	return fmt.Errorf("切换后重启 mihomo 失败，已尝试回滚原版本: %w", restartErr)
}
