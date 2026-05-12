package mihomotui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Daemon IPC 服务端
type Daemon struct {
	mu              sync.RWMutex
	listener        net.Listener
	server          *http.Server
	mihomoAPI       *MihomoAPI
	mihomoProcess   *MihomoProcess
	upgradeProgress UpgradeProgress
}

// socketPath 返回 UDS socket 文件路径（固定路径，支持 root server + 普通用户 TUI）
func socketPath() string {
	return "/tmp/mihomo-tui/daemon.sock"
}

// SocketPath 返回 UDS socket 文件路径（导出给 ui 包使用）
func SocketPath() string {
	return socketPath()
}

// RunDaemon 启动 IPC 后台服务
func RunDaemon() error {
	d := &Daemon{}
	return d.Run()
}

// Run 启动守护进程
func (d *Daemon) Run() error {
	// 独立服务端（非一体模式）且 root 用户，使用 /var 路径
	launchMode := os.Getenv("MIHOMO_TUI_LAUNCH_MODE")
	isEmbedded := launchMode == "embedded"
	if !isEmbedded && os.Geteuid() == 0 && customConfigDir == "" {
		customConfigDir = "/var/lib/mihomo-tui"
		if err := os.MkdirAll(customConfigDir, 0755); err != nil {
			return fmt.Errorf("创建 /var/lib/mihomo-tui 失败: %w", err)
		}
		globalConfig = LoadConfig()
		logDir := filepath.Join(customConfigDir, "logs")
		if err := InitLogger(logDir, globalConfig.LogLevel); err != nil {
			return fmt.Errorf("初始化日志失败: %w", err)
		}
		fmt.Printf("[mihomo-tui server] 日志目录: %s/mihomo-tui-%s.log\n", logDir, time.Now().Format("20060102"))
		Infof("root 独立服务端，配置目录: %s，日志目录: %s", customConfigDir, logDir)
	}

	// 确保配置目录存在
	configDir := GetConfigDir()
	if configDir == "" {
		return fmt.Errorf("配置目录未初始化")
	}

	// 初始化全局配置（服务端独占）
	cfg := GlobalConfig()
	Infof("守护进程启动，配置目录: %s", configDir)

	// 确保 API secret 已设置（mihomo external-controller 需要认证）
	if cfg.Mihomo.Secret == "" {
		cfg.Mihomo.Secret = generateRandomSecret()
		if err := cfg.Flush(); err != nil {
			Warnf("保存 API secret 失败: %v", err)
		} else {
			Infof("已生成 API secret")
		}
	}

	// 自动创建 mihomo 工作目录
	mihomoDir := filepath.Join(configDir, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		Warnf("创建 mihomo 工作目录失败: %v", err)
	}

	// 初始化 mihomo API 客户端
	d.mihomoAPI = NewMihomoAPIFromConfig()

	// 初始化 mihomo 进程管理器
	d.mihomoProcess = NewMihomoProcess()

	// 确保 socket 父目录存在（权限 0777，确保任何用户都能清理旧 socket）
	sock := socketPath()
	sockDir := filepath.Dir(sock)
	if err := os.MkdirAll(sockDir, 0777); err != nil {
		return fmt.Errorf("创建 socket 目录失败: %w", err)
	}
	if err := os.Chmod(sockDir, 0777); err != nil {
		Warnf("设置 socket 目录权限失败: %v", err)
	}

	// 清理旧 socket
	if err := os.Remove(sock); err != nil && !os.IsNotExist(err) {
		// 如果无法删除，检查是否已有 daemon 在监听
		if conn, dialErr := net.Dial("unix", sock); dialErr == nil {
			conn.Close()
			return fmt.Errorf("IPC 服务已在运行: %s", sock)
		}
		// 无法删除且没有 daemon 在监听，可能是权限问题
		return fmt.Errorf("无法清理旧 socket %s: %w", sock, err)
	}

	// 创建 UDS listener
	listener, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("监听 Unix socket %s 失败: %w", sock, err)
	}
	defer listener.Close()

	// 设置 socket 权限：root 启动时 0666（允许任何用户连接），普通用户 0660
	sockPerm := os.FileMode(0660)
	if os.Geteuid() == 0 {
		sockPerm = 0666
	}
	if err := os.Chmod(sock, sockPerm); err != nil {
		Warnf("设置 socket 权限失败: %v", err)
	}

	d.listener = listener
	d.server = &http.Server{
		Handler: d.router(),
	}

	Infof("IPC 服务已启动: %s", sock)
	fmt.Printf("[mihomo-tui server] IPC 服务已启动: %s\n", sock)
	fmt.Println("[mihomo-tui server] 按 Ctrl+C 停止服务")
	return d.server.Serve(listener)
}

// Stop 停止守护进程
func (d *Daemon) Stop() error {
	if d.server != nil {
		return d.server.Shutdown(context.Background())
	}
	return nil
}

// router 返回 HTTP 路由
func (d *Daemon) router() http.Handler {
	mux := http.NewServeMux()

	// 配置
	mux.HandleFunc("/api/v1/config", d.handleConfig)

	// 订阅
	mux.HandleFunc("/api/v1/subscriptions", d.handleSubscriptions)
	mux.HandleFunc("/api/v1/subscriptions/", d.handleSubscriptionDetail)

	// 规则订阅
	mux.HandleFunc("/api/v1/rule-providers", d.handleRuleProviders)
	mux.HandleFunc("/api/v1/rule-providers/", d.handleRuleProviderDetail)

	// mihomo 管理
	mux.HandleFunc("/api/v1/mihomo/status", d.handleMihomoStatus)
	mux.HandleFunc("/api/v1/mihomo/start", d.handleMihomoStart)
	mux.HandleFunc("/api/v1/mihomo/stop", d.handleMihomoStop)
	mux.HandleFunc("/api/v1/mihomo/restart", d.handleMihomoRestart)
	mux.HandleFunc("/api/v1/mihomo/upgrade", d.handleMihomoUpgrade)
	mux.HandleFunc("/api/v1/mihomo/upgrade/progress", d.handleMihomoUpgradeProgress)
	mux.HandleFunc("/api/v1/mihomo/version", d.handleMihomoVersion)
	mux.HandleFunc("/api/v1/mihomo/latest-version", d.handleMihomoLatestVersion)
	mux.HandleFunc("/api/v1/mihomo/external-resources", d.handleExternalResources)
	mux.HandleFunc("/api/v1/mihomo/external-resources/download", d.handleDownloadExternalResources)

	// 心跳
	mux.HandleFunc("/api/v1/ping", d.handlePing)

	// 守护进程信息
	mux.HandleFunc("/api/v1/daemon/info", d.handleDaemonInfo)
	mux.HandleFunc("/api/v1/daemon/config-dir", d.handleDaemonConfigDir)
	mux.HandleFunc("/api/v1/daemon/shutdown", d.handleDaemonShutdown)

	return mux
}

// handleDaemonInfo 返回守护进程信息（启动模式、权限等）
func (d *Daemon) handleDaemonInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	launchMode := os.Getenv("MIHOMO_TUI_LAUNCH_MODE")
	if launchMode == "" {
		launchMode = "standalone"
	}
	info := DaemonInfo{
		LaunchMode: launchMode,
		IsRoot:     os.Geteuid() == 0,
	}
	writeJSON(w, http.StatusOK, ok(info))
}

// handleDaemonConfigDir 返回守护进程使用的配置目录
func (d *Daemon) handleDaemonConfigDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok(map[string]string{"config_dir": GetConfigDir()}))
}

// handleDaemonShutdown 停止守护进程自身
func (d *Daemon) handleDaemonShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok("shutdown"))
	// 在响应发送后异步停止 server
	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := d.Stop(); err != nil {
			Errorf("守护进程停止失败: %v", err)
		}
	}()
}

// ========== 辅助函数 ==========

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, APIResponse{Success: false, Error: err.Error()})
}

func readJSON(r *http.Request, dest any) error {
	return json.NewDecoder(r.Body).Decode(dest)
}

func ok(data any) APIResponse {
	return APIResponse{Success: true, Data: data}
}

// ========== 配置 ==========

func (d *Daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(ConfigResponse{Config: *cfg}))
	case http.MethodPost:
		var req Config
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析配置失败: %w", err))
			return
		}
		Infof("收到配置更新请求: system.tun=%v proxy_mode=%s", req.System.TUN, req.ProxyMode)
		oldTUN := GlobalConfig().System.TUN
		d.mu.Lock()
		globalConfig = req
		if err := globalConfig.Flush(); err != nil {
			d.mu.Unlock()
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存配置失败: %w", err))
			return
		}
		newTUN := globalConfig.System.TUN
		Infof("配置已保存到文件: system.tun=%v proxy_mode=%s", globalConfig.System.TUN, globalConfig.ProxyMode)
		d.mu.Unlock()

		// 配置变更后自动重新生成 mihomo 配置并热重载
		go func() {
			cfg := GlobalConfig()
			if err := cfg.GenerateMihomoConfig(); err != nil {
				Errorf("配置变更后重新生成 mihomo 配置失败: %v", err)
				return
			}
			Infof("配置变更，已重新生成 mihomo 配置")

			// 若 mihomo 正在运行，尝试热重载
			if d.mihomoProcess != nil && d.mihomoProcess.IsRunning() {
				if d.mihomoAPI != nil {
					if err := d.mihomoAPI.ReloadConfigs(true); err != nil {
						Warnf("热重载 mihomo 失败: %v，尝试重启", err)
						if err := d.mihomoProcess.Restart(); err != nil {
							Errorf("重启 mihomo 失败: %v", err)
						}
					} else {
						Infof("mihomo 配置已热重载")
						// 热重载成功且 TUN 状态发生变化时，同步设置/清理路由修复规则
						if oldTUN != newTUN {
							if newTUN {
								if err := SetupTUNRouting(); err != nil {
									Warnf("TUN 路由修复设置失败（外部入站连接可能受影响）: %v", err)
								}
							} else {
								if err := RestoreTUNRouting(); err != nil {
									Warnf("TUN 路由规则清理失败: %v", err)
								}
							}
						}
					}
				}
			}
		}()

		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// ========== 订阅 ==========

func (d *Daemon) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.Subscriptions))
	case http.MethodPost:
		var req SubscriptionImportRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("订阅链接不能为空"))
			return
		}
		if err := d.importSubscription(req.URL); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("导入订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// importSubscription 下载并导入订阅
func (d *Daemon) importSubscription(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载订阅失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	name := "订阅"

	d.mu.Lock()
	defer d.mu.Unlock()
	cfg := GlobalConfig()
	// 如果已存在同名订阅，更新 URL；否则取一个唯一名称
	idx := cfg.FindSubscriptionByName(name)
	if idx >= 0 {
		cfg.Subscriptions[idx].URL = url
		cfg.Subscriptions[idx].UpdatedAt = time.Now().Format("2006-01-02 15:04")
	} else {
		// 尝试生成唯一名称
		baseName := name
		for i := 1; i <= 100; i++ {
			if cfg.FindSubscriptionByName(name) < 0 {
				break
			}
			name = fmt.Sprintf("%s%d", baseName, i)
		}
		cfg.Subscriptions = append(cfg.Subscriptions, SubscriptionMeta{
			Name:      name,
			URL:       url,
			UpdatedAt: time.Now().Format("2006-01-02 15:04"),
		})
	}
	if err := cfg.Flush(); err != nil {
		return fmt.Errorf("保存订阅失败: %w", err)
	}
	Infof("订阅导入成功: name=%s url=%s", name, url)
	return nil
}

func (d *Daemon) handleSubscriptionDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/subscriptions/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("缺少订阅名称"))
		return
	}
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case http.MethodPut:
		// 刷新订阅
		_ = name
		writeJSON(w, http.StatusOK, ok("订阅刷新已接收"))
	case http.MethodDelete:
		// 删除订阅
		cfg := GlobalConfig()
		d.mu.Lock()
		newSubs := make([]SubscriptionMeta, 0, len(cfg.Subscriptions))
		for _, sub := range cfg.Subscriptions {
			if sub.Name != name {
				newSubs = append(newSubs, sub)
			}
		}
		cfg.Subscriptions = newSubs
		_ = cfg.Flush()
		d.mu.Unlock()
		writeJSON(w, http.StatusOK, ok(nil))
	case http.MethodPost:
		if action == "apply" {
			// 应用订阅：先设为激活订阅，再生成配置
			cfg := GlobalConfig()
			d.mu.Lock()
			idx := cfg.FindSubscriptionByName(name)
			if idx < 0 {
				d.mu.Unlock()
				writeError(w, http.StatusBadRequest, fmt.Errorf("订阅不存在: %s", name))
				return
			}
			cfg.ActiveSubscription = idx
			if err := cfg.Flush(); err != nil {
				d.mu.Unlock()
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存激活订阅失败: %w", err))
				return
			}
			if err := cfg.GenerateMihomoConfig(); err != nil {
				d.mu.Unlock()
				writeError(w, http.StatusInternalServerError, fmt.Errorf("生成配置失败: %w", err))
				return
			}
			// 重新创建 mihomo API 客户端以使用最新的 secret
			d.mihomoAPI = NewMihomoAPIFromConfig()
			d.mu.Unlock()
			writeJSON(w, http.StatusOK, ok(nil))
		} else {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作: %s", action))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// ========== 规则订阅 ==========

func (d *Daemon) handleRuleProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.RuleProviderSubscriptions))
	case http.MethodPost:
		var req RuleProviderImportRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("规则订阅链接不能为空"))
			return
		}
		name := req.Name
		if name == "" {
			name = "规则订阅"
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		// 生成唯一名称
		baseName := name
		for i := 1; i <= 100; i++ {
			if cfg.FindRuleProviderByName(name) < 0 {
				break
			}
			name = fmt.Sprintf("%s%d", baseName, i)
		}
		behavior := req.Behavior
		if behavior == "" {
			behavior = "classical"
		}
		format := req.Format
		if format == "" {
			format = "yaml"
		}
		interval := req.Interval
		if interval <= 0 {
			interval = 86400
		}
		proxyGroup := req.ProxyGroup
		if proxyGroup == "" {
			proxyGroup = "Auto"
		}
		cfg.RuleProviderSubscriptions = append(cfg.RuleProviderSubscriptions, RuleProviderSubscription{
			Name:       name,
			URL:        req.URL,
			Behavior:   behavior,
			Format:     format,
			Interval:   interval,
			ProxyGroup: proxyGroup,
			UpdatedAt:  time.Now().Format("2006-01-02 15:04"),
		})
		if err := cfg.Flush(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存规则订阅失败: %w", err))
			return
		}
		Infof("规则订阅导入成功: name=%s url=%s behavior=%s", name, req.URL, behavior)
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

func (d *Daemon) handleRuleProviderDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rule-providers/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("缺少规则订阅名称"))
		return
	}
	name := parts[0]

	switch r.Method {
	case http.MethodPut:
		// 刷新规则订阅：更新 UpdatedAt
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		idx := cfg.FindRuleProviderByName(name)
		if idx < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("规则订阅不存在: %s", name))
			return
		}
		cfg.RuleProviderSubscriptions[idx].UpdatedAt = time.Now().Format("2006-01-02 15:04")
		if err := cfg.Flush(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok("规则订阅刷新已接收"))
	case http.MethodDelete:
		// 删除规则订阅
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		newRps := make([]RuleProviderSubscription, 0, len(cfg.RuleProviderSubscriptions))
		for _, rp := range cfg.RuleProviderSubscriptions {
			if rp.Name != name {
				newRps = append(newRps, rp)
			}
		}
		cfg.RuleProviderSubscriptions = newRps
		_ = cfg.Flush()
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// ========== mihomo 进程管理 ==========

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
	_ = readJSON(r, &req)

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

func (d *Daemon) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok("pong"))
}
