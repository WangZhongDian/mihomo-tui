//go:build !linux

package mihomotui

import "fmt"

// SetupTUNRouting 非 Linux 平台为空实现
func SetupTUNRouting() error { return nil }

// RestoreTUNRouting 非 Linux 平台为空实现
func RestoreTUNRouting() error { return nil }

// DescribeTUNRouting 在非 Linux 平台不支持。
func DescribeTUNRouting() ([]string, error) {
	return nil, fmt.Errorf("当前平台不支持 TUN 路由诊断")
}

// CleanupEnvironment 非 Linux 平台仅清理当前用户的系统代理环境变量。
func CleanupEnvironment() {
	if err := CleanupSystemProxyEnv(); err != nil {
		Warnf("清理系统代理环境变量失败: %v", err)
	}
}
