package mihomotui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	if !isEmbedded && os.Geteuid() == 0 && GetCustomConfigDir() == "" {
		SetCustomConfigDir("/var/lib/mihomo-tui")
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
	if err := os.MkdirAll(mihomoDir, 0700); err != nil {
		Warnf("创建 mihomo 工作目录失败: %v", err)
	} else if err := os.Chmod(mihomoDir, 0700); err != nil {
		Warnf("收紧 mihomo 工作目录权限失败: %v", err)
	}

	// 初始化 mihomo API 客户端
	d.mihomoAPI = NewMihomoAPIFromConfig()

	// 初始化 mihomo 进程管理器
	d.mihomoProcess = NewMihomoProcess()

	// 初始化 IPC 授权器，并以最小权限创建 socket 目录。root daemon 只允许
	// mihomo-tui 组成员通过 socket 访问；普通 daemon 则只允许启动它的用户访问。
	authorizer, err := newIPCAuthorizer()
	if err != nil {
		return fmt.Errorf("初始化 IPC 授权失败: %w", err)
	}
	sock := daemonSocketPath()
	sockDir := filepath.Dir(sock)
	if err := os.MkdirAll(sockDir, 0750); err != nil {
		return fmt.Errorf("创建 socket 目录失败: %w", err)
	}
	if err := authorizer.configureSocketDirectory(sockDir); err != nil {
		return err
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

	if err := authorizer.configureSocketPermissions(sock); err != nil {
		return fmt.Errorf("设置 IPC socket 权限失败: %w", err)
	}

	d.listener = listener
	d.server = &http.Server{
		Handler:     authorizer.middleware(d.router()),
		ConnContext: authorizer.connContext,
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
	mux.HandleFunc("/api/v1/mihomo/api-credentials", d.handleMihomoAPICredentials)
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

// ========== 守护进程自身 Handler ==========

func (d *Daemon) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok("pong"))
}

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

func (d *Daemon) handleDaemonConfigDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
		return
	}
	writeJSON(w, http.StatusOK, ok(map[string]string{"config_dir": GetConfigDir()}))
}

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
