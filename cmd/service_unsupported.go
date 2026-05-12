//go:build !linux

package cmd

import "fmt"

// InstallService 非 Linux 平台不支持安装 systemd 服务
func InstallService() error {
	return fmt.Errorf("install_service 仅在 Linux 平台支持")
}

// UninstallService 非 Linux 平台不支持卸载 systemd 服务
func UninstallService() error {
	return fmt.Errorf("uninstall 仅在 Linux 平台支持")
}
