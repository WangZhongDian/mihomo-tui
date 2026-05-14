package cmd

import (
	"flag"
	"fmt"
	"os"

	"mihomotui/mihomotui"
	"mihomotui/mihomotui/ui"
)

// Version 通过 mihomotui.Version 统一维护

// RunTUI 启动 TUI 客户端
func RunTUI(dir string, standalone bool) {
	if dir != "" {
		mihomotui.SetCustomConfigDir(dir)
	}
	if err := ui.Run(standalone); err != nil {
		fmt.Fprintf(os.Stderr, "启动 TUI 失败: %v\n", err)
		os.Exit(1)
	}
}

// RunServer 启动 IPC 服务
func RunServer(args []string, globalDir string) {
	serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
	serverDir := serverFlags.String("d", "", "指定配置目录")
	serverFlags.Parse(args)

	if *serverDir != "" {
		mihomotui.SetCustomConfigDir(*serverDir)
	} else if globalDir != "" {
		mihomotui.SetCustomConfigDir(globalDir)
	}

	if err := mihomotui.RunDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "启动服务失败: %v\n", err)
		os.Exit(1)
	}
}

// RunInstallService 安装 systemd 服务
func RunInstallService(dir string) {
	if dir != "" {
		mihomotui.SetCustomConfigDir(dir)
	}
	if err := InstallService(); err != nil {
		fmt.Fprintf(os.Stderr, "安装服务失败: %v\n", err)
		os.Exit(1)
	}
}

// RunUninstallService 卸载 systemd 服务
func RunUninstallService() {
	if err := UninstallService(); err != nil {
		fmt.Fprintf(os.Stderr, "卸载服务失败: %v\n", err)
		os.Exit(1)
	}
}

// RunVersion 输出版本信息
func RunVersion() {
	fmt.Printf("mihomo-tui %s\n", mihomotui.Version)
}

// RunCleanup 清理系统代理环境变量和 TUN 路由规则
func RunCleanup() {
	mihomotui.CleanupEnvironment()
	fmt.Println("✅ 环境清理完成")
}
