package mihomotui

import (
	"os"
	"testing"
)

// TestClientPreferencesMigratesFromSharedConfig 首次加载且偏好文件不存在时，
// 从旧版共享配置迁移 system_proxy，升级后开关状态不丢失。
func TestClientPreferencesMigratesFromSharedConfig(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.System.SystemProxy = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if got := GetSystemProxyPreference(); got != true {
		t.Fatalf("migrated SystemProxy preference = %v, want true", got)
	}
}

// TestClientPreferencesPersistAndReload 偏好修改应原子落盘并在重新加载后保持，
// 且不受后续共享配置变化影响（客户端/服务端配置边界）。
func TestClientPreferencesPersistAndReload(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	// 迁移后的初始值来自共享配置；将其取反以产生真实的偏好变更。
	initial := GetSystemProxyPreference()
	want := !initial
	if err := SetSystemProxyPreference(want); err != nil {
		t.Fatalf("SetSystemProxyPreference() error = %v", err)
	}

	info, err := os.Stat(preferencesFilePath())
	if err != nil {
		t.Fatalf("preferences file not written: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("preferences file permission = %04o, want 0600", got)
	}

	// 共享配置中该标志随后被改回 initial（模拟另一会话），重新加载偏好不应受影响。
	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.System.SystemProxy = initial
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resetClientPreferencesForTest()
	if got := GetSystemProxyPreference(); got != want {
		t.Fatalf("reloaded SystemProxy preference = %v, want %v（偏好应独立于共享配置）", got, want)
	}
}

// TestSetSystemProxyPreferenceIdempotent 设置为当前相同值时不产生写入。
func TestSetSystemProxyPreferenceIdempotent(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	current := GetSystemProxyPreference()
	if err := SetSystemProxyPreference(current); err != nil {
		t.Fatalf("SetSystemProxyPreference(%v) error = %v", current, err)
	}
	if _, err := os.Stat(preferencesFilePath()); !os.IsNotExist(err) {
		t.Fatal("idempotent no-op should not create preferences file")
	}
}
