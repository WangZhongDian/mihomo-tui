//go:build !linux

package mihomotui

// SetupTUNRouting 非 Linux 平台为空实现
func SetupTUNRouting() error { return nil }

// RestoreTUNRouting 非 Linux 平台为空实现
func RestoreTUNRouting() error { return nil }
