package mihomotui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestGlobalConfigReturnsIsolatedSnapshot 验证 GlobalConfig 返回深拷贝：
// 调用方修改快照不会影响全局状态（P1：全局可变指针污染修复）。
func TestGlobalConfigReturnsIsolatedSnapshot(t *testing.T) {
	useTestConfigDir(t)

	snap := GlobalConfig()
	snap.ProxyMode = "global"
	snap.Subscriptions = append(snap.Subscriptions, SubscriptionMeta{ID: "x", Name: "x", URL: "https://example.com/sub"})
	snap.CustomRules = append(snap.CustomRules, "DOMAIN,example.com,Auto")

	if got := GlobalConfig().ProxyMode; got != "rule" {
		t.Fatalf("global ProxyMode polluted by snapshot mutation: %q", got)
	}
	if got := len(GlobalConfig().Subscriptions); got != 0 {
		t.Fatalf("global Subscriptions polluted by snapshot mutation: %d", got)
	}
	if got := len(GlobalConfig().CustomRules); got != 0 {
		t.Fatalf("global CustomRules polluted by snapshot mutation: %d", got)
	}
}

// TestUpdateGlobalConfigCommitsAtomically 验证提交成功后版本递增且内存与磁盘一致。
func TestUpdateGlobalConfigCommitsAtomically(t *testing.T) {
	useTestConfigDir(t)

	committed, err := UpdateGlobalConfig(func(c *Config) error {
		c.ProxyMode = "global"
		c.CustomRules = append(c.CustomRules, "DOMAIN,example.com,Auto")
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateGlobalConfig() error = %v", err)
	}
	if committed.Version != 1 {
		t.Fatalf("committed version = %d, want 1", committed.Version)
	}
	if got := GlobalConfig().ProxyMode; got != "global" {
		t.Fatalf("in-memory ProxyMode = %q, want global", got)
	}
	loaded := LoadConfig()
	if loaded.ProxyMode != "global" || loaded.Version != 1 || len(loaded.CustomRules) != 1 {
		t.Fatalf("on-disk config mismatch: mode=%q version=%d rules=%d", loaded.ProxyMode, loaded.Version, len(loaded.CustomRules))
	}
}

// TestUpdateGlobalConfigFnErrorKeepsState 验证 fn 返回错误时不产生任何提交。
func TestUpdateGlobalConfigFnErrorKeepsState(t *testing.T) {
	useTestConfigDir(t)

	_, err := UpdateGlobalConfig(func(c *Config) error {
		c.ProxyMode = "global"
		return fmt.Errorf("模拟修改失败")
	})
	if err == nil || !strings.Contains(err.Error(), "模拟修改失败") {
		t.Fatalf("UpdateGlobalConfig() error = %v", err)
	}
	if got := GlobalConfig(); got.ProxyMode != "rule" || got.Version != 0 {
		t.Fatalf("state changed after fn error: mode=%q version=%d", got.ProxyMode, got.Version)
	}
}

// TestUpdateGlobalConfigValidationFailureKeepsState 验证校验失败时内存与磁盘均保持旧值。
func TestUpdateGlobalConfigValidationFailureKeepsState(t *testing.T) {
	useTestConfigDir(t)

	_, err := UpdateGlobalConfig(func(c *Config) error {
		c.Mihomo.HTTPPort = 8080
		c.Mihomo.MixedPort = 8080 // 端口冲突
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "端口冲突") {
		t.Fatalf("UpdateGlobalConfig() error = %v, want 端口冲突", err)
	}
	cfg := GlobalConfig()
	if cfg.Mihomo.HTTPPort != 7890 || cfg.Mihomo.MixedPort != 7892 || cfg.Version != 0 {
		t.Fatalf("memory changed after validation failure: %+v", cfg.Mihomo)
	}
}

// TestUpdateGlobalConfigFlushFailureKeepsMemoryAndDisk 验证落盘失败时内存保持旧值（P1-2 验收标准）。
func TestUpdateGlobalConfigFlushFailureKeepsMemoryAndDisk(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 可绕过目录权限，无法模拟落盘失败")
	}
	useTestConfigDir(t)

	dir := GetConfigDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	_, err := UpdateGlobalConfig(func(c *Config) error {
		c.ProxyMode = "global"
		return nil
	})
	if err == nil {
		t.Fatal("UpdateGlobalConfig() unexpectedly succeeded with read-only config dir")
	}
	if got := GlobalConfig(); got.ProxyMode != "rule" || got.Version != 0 {
		t.Fatalf("memory changed after flush failure: mode=%q version=%d", got.ProxyMode, got.Version)
	}
}

// TestReplaceGlobalConfigDetectsStaleVersion 验证乐观并发控制（P1-5 验收标准）。
func TestReplaceGlobalConfigDetectsStaleVersion(t *testing.T) {
	useTestConfigDir(t)

	cfg := defaultConfig()
	cfg.Mihomo.Secret = "s3cret"
	SetGlobalConfig(cfg)

	// 基于当前版本提交 → 成功
	req := *GlobalConfig()
	req.ProxyMode = "direct"
	req.Mihomo.Secret = "" // 模拟客户端提交掩码后的配置
	committed, err := ReplaceGlobalConfig(req)
	if err != nil {
		t.Fatalf("ReplaceGlobalConfig() error = %v", err)
	}
	if committed.Version != 1 || committed.ProxyMode != "direct" {
		t.Fatalf("unexpected committed config: version=%d mode=%q", committed.Version, committed.ProxyMode)
	}
	if committed.Mihomo.Secret != "s3cret" {
		t.Fatalf("masked secret was not preserved: %q", committed.Mihomo.Secret)
	}

	// 基于过期版本提交 → 冲突
	stale := req // version 仍为 0
	stale.ProxyMode = "rule"
	_, err = ReplaceGlobalConfig(stale)
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("ReplaceGlobalConfig() error = %v, want ErrConfigConflict", err)
	}
	if got := GlobalConfig().ProxyMode; got != "direct" {
		t.Fatalf("conflicting commit changed state: %q", got)
	}
}

// TestConcurrentConfigCommitsSerialize 验证并发提交串行化（配合 -race 验证锁正确性）。
func TestConcurrentConfigCommitsSerialize(t *testing.T) {
	useTestConfigDir(t)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rule := fmt.Sprintf("DOMAIN,example%d.com,Auto", i)
			if _, err := UpdateGlobalConfig(func(c *Config) error {
				c.CustomRules = append(c.CustomRules, rule)
				return nil
			}); err != nil {
				t.Errorf("UpdateGlobalConfig() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	cfg := GlobalConfig()
	if got := len(cfg.CustomRules); got != n {
		t.Fatalf("custom rules = %d, want %d（并发提交丢失）", got, n)
	}
	if cfg.Version != int64(n) {
		t.Fatalf("version = %d, want %d", cfg.Version, n)
	}
	loaded := LoadConfig()
	if len(loaded.CustomRules) != n {
		t.Fatalf("on-disk custom rules = %d, want %d（内存与磁盘不一致）", len(loaded.CustomRules), n)
	}
}

// TestReplaceGlobalConfigRootDaemonPreservesLocalPaths 验证 root daemon（多用户共享）
// 不接受客户端提交的本机路径字段，普通用户 daemon 则接受（P1 第 7 节字段边界）。
func TestReplaceGlobalConfigRootDaemonPreservesLocalPaths(t *testing.T) {
	useTestConfigDir(t)

	oldRootCheck := daemonRunsAsRoot
	t.Cleanup(func() { daemonRunsAsRoot = oldRootCheck })

	cfg := defaultConfig()
	cfg.MihomoBinaryPath = "/var/lib/mihomo-tui/bin/mihomo"
	cfg.LogDir = "/var/lib/mihomo-tui/logs"
	SetGlobalConfig(cfg)

	// root daemon：客户端路径被忽略，daemon 本机路径保留
	daemonRunsAsRoot = func() bool { return true }
	req := *GlobalConfig()
	req.MihomoBinaryPath = "/home/alice/.config/mihomo-tui/bin/mihomo"
	req.LogDir = "/home/alice/.config/mihomo-tui/logs"
	committed, err := ReplaceGlobalConfig(req)
	if err != nil {
		t.Fatalf("ReplaceGlobalConfig() error = %v", err)
	}
	if committed.MihomoBinaryPath != "/var/lib/mihomo-tui/bin/mihomo" {
		t.Fatalf("root daemon accepted client binary path: %q", committed.MihomoBinaryPath)
	}
	if committed.LogDir != "/var/lib/mihomo-tui/logs" {
		t.Fatalf("root daemon accepted client log dir: %q", committed.LogDir)
	}

	// 普通用户 daemon（standalone 同用户场景）：接受路径更新
	daemonRunsAsRoot = func() bool { return false }
	req = *GlobalConfig()
	req.LogDir = "/home/alice/custom-logs"
	committed, err = ReplaceGlobalConfig(req)
	if err != nil {
		t.Fatalf("ReplaceGlobalConfig() error = %v", err)
	}
	if committed.LogDir != "/home/alice/custom-logs" {
		t.Fatalf("user daemon should accept log dir update: %q", committed.LogDir)
	}
}
