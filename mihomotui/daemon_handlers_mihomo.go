package mihomotui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

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
	version, err := GetMihomoLastVersion()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("获取最新版本失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, ok(map[string]string{"version": version}))
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
