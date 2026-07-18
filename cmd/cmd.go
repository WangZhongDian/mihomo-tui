package cmd

import (
	"flag"
	"fmt"
	"io"
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

// RunGrantOperator 将指定用户加入 root daemon 的订阅管理授权组。
func RunGrantOperator(userName string) {
	if err := AddIPCOperator(userName); err != nil {
		fmt.Fprintf(os.Stderr, "授权 IPC 用户失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 已验证用户 %s 已加入 mihomo-tui IPC 授权组。\n", userName)
	fmt.Println("   组权限只会在新的登录会话中生效；请注销桌面会话并重新登录（仅关闭终端窗口不算）。")
	fmt.Println("   临时生效可执行: newgrp mihomo-tui")
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
// RunTUNDiagnose 输出 TUN 路由修复的 dry-run 计划，不会修改系统网络。
func RunTUNDiagnose() {
	commands, err := mihomotui.DescribeTUNRouting()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUN 路由诊断失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("mihomo-tui TUN 路由 dry-run（未执行任何修改）：")
	for _, command := range commands {
		fmt.Println("  " + command)
	}
}

func RunCleanup() {
	if os.Geteuid() == 0 && mihomotui.GetCustomConfigDir() == "" {
		mihomotui.SetCustomConfigDir("/var/lib/mihomo-tui")
	}
	mihomotui.CleanupEnvironment()
	fmt.Println("✅ 环境清理完成")
}

// RunSubscriptionImport 从 URL、文件或 stdin 导入订阅正文；不会执行用户提供的 shell 命令。
func RunSubscriptionImport(args []string, globalDir string) {
	fs := flag.NewFlagSet("subscription import", flag.ExitOnError)
	name := fs.String("name", "", "订阅显示名称")
	urlValue := fs.String("url", "", "远程订阅 URL")
	fileValue := fs.String("file", "", "本地订阅文件")
	stdin := fs.Bool("stdin", false, "从标准输入读取订阅正文")
	useProxy := fs.Bool("via-local-proxy", false, "刷新远端 URL 时通过本地 mihomo HTTP 代理")
	fs.Parse(args)
	if globalDir != "" {
		mihomotui.SetCustomConfigDir(globalDir)
	}
	if (*urlValue != "" && (*fileValue != "" || *stdin)) || (*fileValue != "" && *stdin) || (*urlValue == "" && *fileValue == "" && !*stdin) {
		fmt.Fprintln(os.Stderr, "必须且只能指定 --url、--file 或 --stdin")
		os.Exit(2)
	}
	client, err := mihomotui.GetIPCClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 IPC 服务失败: %v\n", err)
		os.Exit(1)
	}
	if *urlValue != "" {
		err = client.IPCImportSubscriptionWithRequest(mihomotui.SubscriptionImportRequest{Name: *name, URL: *urlValue, SourceType: mihomotui.SubscriptionSourceURL, UseLocalProxy: *useProxy})
	} else {
		var data []byte
		if *stdin {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(*fileValue)
		}
		if err == nil {
			source := *fileValue
			if *stdin {
				source = "stdin"
			}
			err = client.IPCImportSubscriptionContent(*name, source, mihomotui.SubscriptionSourceFile, string(data), false)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "导入订阅失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 订阅已导入并由 mihomo-tui 主动接管")
}
