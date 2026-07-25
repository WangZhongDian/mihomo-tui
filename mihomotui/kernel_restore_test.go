package mihomotui

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseKernelWasRunningHint(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
		ok   bool
	}{
		{"1", true, true},
		{"true", true, true},
		{"YES", true, true},
		{"0", false, true},
		{"false", false, true},
		{"no", false, true},
		{"", false, false},
		{"maybe", false, false},
	}
	for _, tc := range cases {
		got, ok := ParseKernelWasRunningHint(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("ParseKernelWasRunningHint(%q) = (%v, %v), want (%v, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveKernelWasRunningPrefersHint(t *testing.T) {
	t.Setenv(KernelWasRunningEnv, "0")
	if !ResolveKernelWasRunning("true") {
		t.Fatal("hint true should win over env 0")
	}
	if ResolveKernelWasRunning("0") {
		t.Fatal("hint 0 should be false")
	}
}

func TestResolveKernelWasRunningUsesEnv(t *testing.T) {
	t.Setenv(KernelWasRunningEnv, "1")
	if !ResolveKernelWasRunning("") {
		t.Fatal("env 1 should mean was running")
	}
	t.Setenv(KernelWasRunningEnv, "0")
	if ResolveKernelWasRunning("") {
		t.Fatal("env 0 should mean not running")
	}
}

func TestRestoreKernelIfNeededSkipsWhenNotRunning(t *testing.T) {
	if err := RestoreKernelIfNeeded(false); err != nil {
		t.Fatalf("RestoreKernelIfNeeded(false) = %v", err)
	}
}

func TestProbeKernelRunningWhenDaemonDown(t *testing.T) {
	// Unreachable private socket: should report not running without hard error
	// unless a real root daemon is present on the machine.
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	running, err := ProbeKernelRunning()
	if err != nil {
		t.Fatalf("ProbeKernelRunning() error = %v", err)
	}
	if running {
		t.Log("kernel reported running (local root daemon present); soft skip")
	}
}

func startUnixIPCServer(t *testing.T, statusRunning bool, startCalls *int32) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "daemon.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: "pong"})
	})
	mux.HandleFunc("/api/v1/mihomo/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: MihomoStatusResponse{Running: statusRunning, PID: 42}})
	})
	mux.HandleFunc("/api/v1/mihomo/start", func(w http.ResponseWriter, r *http.Request) {
		if startCalls != nil {
			atomic.AddInt32(startCalls, 1)
		}
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true})
	})
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: defaultConfig()})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

func testIPCClient(sock string) *IPCClient {
	return &IPCClient{
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", sock)
				},
			},
		},
		baseURL:    "http://daemon",
		socketPath: sock,
	}
}

func TestIPCClientStartAndStatusForRestore(t *testing.T) {
	var calls int32
	sock := startUnixIPCServer(t, false, &calls)
	client := testIPCClient(sock)

	status, err := client.IPCGetMihomoStatus()
	if err != nil {
		t.Fatalf("IPCGetMihomoStatus: %v", err)
	}
	if status.Running {
		t.Fatal("status.Running = true, want false")
	}
	if err := client.IPCStartMihomo(); err != nil {
		t.Fatalf("IPCStartMihomo: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("start calls = %d, want 1", calls)
	}
}
