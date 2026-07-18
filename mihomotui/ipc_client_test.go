package mihomotui

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// useTestIPCServer 将 IPC 客户端单例指向测试 HTTP 服务，结束后恢复。
func useTestIPCServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	ipcClientMu.Lock()
	old := ipcClientSingleton
	ipcClientSingleton = &IPCClient{client: server.Client(), baseURL: server.URL}
	ipcClientMu.Unlock()
	t.Cleanup(func() {
		server.Close()
		ipcClientMu.Lock()
		ipcClientSingleton = old
		ipcClientMu.Unlock()
	})
}

// newConfigTestMux 使用真实 daemon 配置 handler 搭建测试路由；
// apply 一律成功，使测试聚焦在版本/提交语义上。
func newConfigTestMux(d *Daemon) *http.ServeMux {
	d.reconcileApply = func(req reconcileRequest) ApplyReport { return ApplyReport{Applied: true} }
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", d.handleConfig)
	mux.HandleFunc("/api/v1/mihomo/api-credentials", d.handleMihomoAPICredentials)
	return mux
}

// TestMutateServerConfigSkipsNoOpMutation 验证无实际变化的修改不会发起提交：
// 页面构建期控件回调、输入框失焦等路径会重复触发保存，跳过提交可避免
// 无意义的版本递增、运行时应用与并发冲突。
func TestMutateServerConfigSkipsNoOpMutation(t *testing.T) {
	useTestConfigDir(t)

	var postCount atomic.Int32
	d := &Daemon{}
	mux := newConfigTestMux(d)
	var wrapped http.Handler = mux
	wrapped = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	useTestIPCServer(t, wrapped)

	current := GlobalConfig().ProxyMode
	resp, err := MutateServerConfig(func(c *Config) {
		c.ProxyMode = current // 与当前值相同，属于无操作修改
	})
	if err != nil {
		t.Fatalf("no-op MutateServerConfig() error = %v", err)
	}
	if !resp.Applied {
		t.Fatalf("no-op response should report applied: %+v", resp)
	}
	if got := postCount.Load(); got != 0 {
		t.Fatalf("no-op mutation triggered %d config POSTs, want 0", got)
	}
	if got := GlobalConfig().Version; got != 0 {
		t.Fatalf("no-op mutation bumped version to %d, want 0", got)
	}
}

// TestMutateServerConfigCommitsRealChange 验证真实变更仍会正常提交并递增版本。
func TestMutateServerConfigCommitsRealChange(t *testing.T) {
	useTestConfigDir(t)

	d := &Daemon{}
	useTestIPCServer(t, newConfigTestMux(d))

	resp, err := MutateServerConfig(func(c *Config) {
		c.ProxyMode = "global"
	})
	if err != nil {
		t.Fatalf("MutateServerConfig() error = %v", err)
	}
	if !resp.Applied {
		t.Fatalf("response = %+v, want applied", resp)
	}
	committed := GlobalConfig()
	if committed.ProxyMode != "global" || committed.Version != 1 {
		t.Fatalf("committed config: mode=%q version=%d, want global/1", committed.ProxyMode, committed.Version)
	}
}

// TestMutateServerConfigSucceedsWhenChangeAlreadyCommitted 模拟页面构建期多个
// 相同保存并发竞争的场景：另一会话抢先提交了相同变更导致首次提交 409，
// 重试时修改变为无操作，应直接成功而不是再次冲突报错。
func TestMutateServerConfigSucceedsWhenChangeAlreadyCommitted(t *testing.T) {
	useTestConfigDir(t)

	d := &Daemon{}
	mux := newConfigTestMux(d)
	var once sync.Once
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 在首次 POST 到达前，模拟另一会话抢先提交相同变更
		if r.Method == http.MethodPost {
			once.Do(func() {
				if _, err := UpdateGlobalConfig(func(c *Config) error {
					c.ProxyMode = "global"
					return nil
				}); err != nil {
					t.Errorf("concurrent commit failed: %v", err)
				}
			})
		}
		mux.ServeHTTP(w, r)
	})
	useTestIPCServer(t, wrapped)

	resp, err := MutateServerConfig(func(c *Config) {
		c.ProxyMode = "global"
	})
	if err != nil {
		t.Fatalf("duplicate change should succeed without conflict error, got: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("response = %+v, want applied", resp)
	}
	if got := GlobalConfig().Version; got != 1 {
		t.Fatalf("version = %d, want 1（仅另一会话的提交生效一次）", got)
	}
}

// TestMutateServerConfigRetriesOnConflict 验证另一会话提交不同变更导致 409 时，
// 客户端重新获取最新配置并重新应用修改后提交成功，两个变更均保留。
func TestMutateServerConfigRetriesOnConflict(t *testing.T) {
	useTestConfigDir(t)

	d := &Daemon{}
	mux := newConfigTestMux(d)
	var once sync.Once
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			once.Do(func() {
				if _, err := UpdateGlobalConfig(func(c *Config) error {
					c.System.AutoStart = true // 另一会话的不同变更
					return nil
				}); err != nil {
					t.Errorf("concurrent commit failed: %v", err)
				}
			})
		}
		mux.ServeHTTP(w, r)
	})
	useTestIPCServer(t, wrapped)

	if _, err := MutateServerConfig(func(c *Config) {
		c.ProxyMode = "global"
	}); err != nil {
		t.Fatalf("conflict retry should succeed, got: %v", err)
	}
	committed := GlobalConfig()
	if committed.ProxyMode != "global" {
		t.Fatalf("local change lost after retry: mode=%q", committed.ProxyMode)
	}
	if !committed.System.AutoStart {
		t.Fatal("other session change lost after retry")
	}
	if committed.Version != 2 {
		t.Fatalf("version = %d, want 2（两次真实提交）", committed.Version)
	}
}

func TestIPCStartMihomoSynchronizesRunningVersion(t *testing.T) {
	useTestConfigDir(t)
	local := *GlobalConfig()
	local.MihomoConfigPath = "/local/mihomo/config.yaml"
	local.MihomoBinaryPath = "/local/bin/mihomo"
	SetGlobalConfig(local)

	serverCfg := local.Clone()
	serverCfg.MihomoConfigPath = "/daemon/mihomo/config.yaml"
	serverCfg.MihomoBinaryPath = "/daemon/bin/mihomo"
	serverCfg.MihomoRunningVersion = "1.19.28"
	serverCfg.MihomoRunningVersionAt = "2026-07-18T16:00:00+08:00"

	useTestIPCServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/mihomo/start":
			if r.Method != http.MethodPost {
				t.Errorf("start method = %s, want POST", r.Method)
			}
			writeJSON(w, http.StatusOK, ok(nil))
		case "/api/v1/config":
			writeJSON(w, http.StatusOK, ok(ConfigResponse{Config: serverCfg}))
		case "/api/v1/mihomo/api-credentials":
			writeJSON(w, http.StatusOK, ok(map[string]string{"external_controller": "127.0.0.1:9090", "secret": "test-secret"}))
		default:
			t.Errorf("unexpected IPC request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	client, err := GetIPCClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.IPCStartMihomo(); err != nil {
		t.Fatalf("IPCStartMihomo() error = %v", err)
	}
	got := GlobalConfig()
	if got.MihomoRunningVersion != "1.19.28" || got.MihomoRunningVersionAt == "" {
		t.Fatalf("running version was not synchronized: %+v", got)
	}
	if got.MihomoConfigPath != local.MihomoConfigPath || got.MihomoBinaryPath != local.MihomoBinaryPath {
		t.Fatalf("local paths must be retained: got config=%q binary=%q", got.MihomoConfigPath, got.MihomoBinaryPath)
	}
}

func TestIPCGetMihomoStatusSynchronizesRunningVersion(t *testing.T) {
	useTestConfigDir(t)
	cfg := *GlobalConfig()
	cfg.MihomoRunningVersion = "1.19.27"
	SetGlobalConfig(cfg)

	useTestIPCServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/mihomo/status" {
			t.Errorf("unexpected IPC request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, ok(MihomoStatusResponse{
			Running: true, PID: 1234, RunningVersion: "1.19.28", VersionAt: "2026-07-18T16:10:00+08:00",
		}))
	}))

	client, err := GetIPCClient()
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.IPCGetMihomoStatus()
	if err != nil {
		t.Fatalf("IPCGetMihomoStatus() error = %v", err)
	}
	if !status.Running || status.RunningVersion != "1.19.28" {
		t.Fatalf("status = %+v", status)
	}
	if got := GlobalConfig().MihomoRunningVersion; got != "1.19.28" {
		t.Fatalf("local running version = %q, want 1.19.28", got)
	}
}
