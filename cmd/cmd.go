package cmd

import (
	"flag"
	"fmt"
	"os"

	"mihomotui/mihomotui"
	"mihomotui/mihomotui/ui"
)

const Version = "v0.1.0"

// RunTUI 启动 TUI 客户端
func RunTUI(dir string, standalone bool) {
	if dir != "" {
		mihomotui.SetCustomConfigDir(dir)
	}
	if err := ui.Run(standalone); err != nil {
		panic(err)
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
	fmt.Printf("mihomo-tui %s\n", Version)
}
