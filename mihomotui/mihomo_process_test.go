package mihomotui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeMihomo 写入一个可执行的假 mihomo 脚本并配置为当前二进制路径。
func writeFakeMihomo(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假 mihomo 脚本依赖 POSIX shell")
	}
	dir := GetConfigDir()
	path := filepath.Join(dir, "bin", "mihomo")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.MihomoBinaryPath = path
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Start 要求 mihomo 配置文件存在
	cfg := GlobalConfig()
	if err := os.MkdirAll(filepath.Dir(cfg.MihomoConfigPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.MihomoConfigPath, []byte("# fake mihomo config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStopReturnsSentinelWhenNotRunning 未运行时 Stop 返回哨兵错误（P1-4）。
func TestStopReturnsSentinelWhenNotRunning(t *testing.T) {
	p := NewMihomoProcess()
	if err := p.Stop(); !errors.Is(err, ErrMihomoNotRunning) {
		t.Fatalf("Stop() error = %v, want ErrMihomoNotRunning", err)
	}
}

// TestRestartCancelsWhenStopFails 停止失败时重启必须返回原因，不得继续误启动（P1-4 验收标准）。
func TestRestartCancelsWhenStopFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖 POSIX 进程信号语义")
	}
	// 构造一个"已回收但状态仍为 running"的进程管理器：
	// 对已回收进程发送信号会失败，模拟停止失败场景。
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	p := NewMihomoProcess()
	p.mu.Lock()
	p.cmd = cmd
	p.running = true
	p.pid = cmd.Process.Pid
	p.exited = make(chan error, 1)
	p.mu.Unlock()

	err := p.Restart()
	if err == nil || !strings.Contains(err.Error(), "已取消重启") {
		t.Fatalf("Restart() error = %v, want 已取消重启", err)
	}
	if strings.Contains(err.Error(), "启动") && !strings.Contains(err.Error(), "已取消重启") {
		t.Fatalf("Restart attempted start after stop failure: %v", err)
	}
}

// TestRestartStartsDirectlyWhenNotRunning 未运行时 Restart 应继续执行启动路径，
// 而不是被 ErrMihomoNotRunning 中断。
func TestRestartStartsDirectlyWhenNotRunning(t *testing.T) {
	useTestConfigDir(t)
	// 将二进制路径配置为一个目录：os.Stat 成功但执行必然失败，
	// 从而确定性地验证 Restart 越过了 Stop 的未运行分支进入 Start。
	dir := t.TempDir()
	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.MihomoBinaryPath = dir
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	p := NewMihomoProcess()
	err := p.Restart()
	if err == nil {
		t.Fatal("Restart() unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "已取消重启") || errors.Is(err, ErrMihomoNotRunning) {
		t.Fatalf("Restart() error = %v, want start-path failure", err)
	}
}

// TestStartReportsEarlyExitWithOutput 启动后立即退出时错误必须包含进程输出（P1-4 验收标准）。
func TestStartReportsEarlyExitWithOutput(t *testing.T) {
	useTestConfigDir(t)
	writeFakeMihomo(t, "#!/bin/sh\necho 'FATAL: bad config line 42' >&2\nexit 1\n")

	p := NewMihomoProcess()
	p.startSettle = 2 * time.Second // 失败路径应立即返回，不等满确认窗口
	start := time.Now()
	err := p.Start()
	if err == nil {
		t.Fatal("Start() unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "FATAL: bad config line 42") {
		t.Fatalf("Start() error missing process output: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Start() took %v for an immediately-exiting process（仍在依赖固定 Sleep）", elapsed)
	}
	if p.IsRunning() {
		t.Fatal("IsRunning() = true after early exit")
	}
}

// TestStartStopRestartLifecycle 正常启动/停止/重启的生命周期状态转换（P1-4 验收标准）。
func TestStartStopRestartLifecycle(t *testing.T) {
	useTestConfigDir(t)
	writeFakeMihomo(t, "#!/bin/sh\nsleep 30\n")

	p := NewMihomoProcess()
	p.startSettle = 150 * time.Millisecond
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	running, pid := p.Status()
	if !running || pid <= 0 {
		t.Fatalf("Status() = (%v, %d), want running with pid", running, pid)
	}
	// 重复启动应被拒绝
	if err := p.Start(); err == nil {
		t.Fatal("second Start() unexpectedly succeeded")
	}
	t.Cleanup(func() { _ = p.Stop() })

	if err := p.Restart(); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if !p.IsRunning() {
		t.Fatal("IsRunning() = false after Restart")
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if p.IsRunning() {
		t.Fatal("IsRunning() = true after Stop")
	}
}

// TestStopForceKillsUnresponsiveProcess SIGTERM 无响应时超时强制终止（P1-4 验收标准）。
func TestStopForceKillsUnresponsiveProcess(t *testing.T) {
	useTestConfigDir(t)
	// 忽略 SIGTERM，验证超时后 SIGKILL 生效
	writeFakeMihomo(t, "#!/bin/sh\ntrap '' TERM\nsleep 30\n")

	p := NewMihomoProcess()
	p.startSettle = 150 * time.Millisecond
	p.stopTimeout = 300 * time.Millisecond
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	start := time.Now()
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop() took %v, force-kill did not happen within timeout", elapsed)
	}
	if p.IsRunning() {
		t.Fatal("IsRunning() = true after force kill")
	}
}

// TestStartUsesConfigRootAsMihomoHome ensures that local subscription caches
// under <config>/subscriptions remain inside mihomo's SAFE_PATHS boundary.
func TestStartUsesConfigRootAsMihomoHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假 mihomo 脚本依赖 POSIX shell")
	}
	useTestConfigDir(t)
	configDir := GetConfigDir()
	configPath := filepath.Join(configDir, "mihomo", MIHOMO_CONFIG_NAME)
	writeFakeMihomo(t, "#!/bin/sh\n"+
		"if [ \"$1\" = \"-d\" ] && [ \"$2\" = \""+configDir+"\" ] && [ \"$3\" = \"-f\" ] && [ \"$4\" = \""+configPath+"\" ]; then\n"+
		"  echo 'arguments verified' >&2\n"+
		"else\n"+
		"  echo \"unexpected arguments: $*\" >&2\n"+
		"fi\n"+
		"exit 1\n")

	err := NewMihomoProcess().Start()
	if err == nil || !strings.Contains(err.Error(), "arguments verified") {
		t.Fatalf("Start() error = %v, want validated -d config root and -f config path", err)
	}
}
