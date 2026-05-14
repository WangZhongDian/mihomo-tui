package main

import (
	"flag"
	"fmt"
	"os"

	"mihomotui/cmd"
)

func main() {
	dir := flag.String("d", "", "指定配置目录")
	standalone := flag.Bool("standalone", false, "启动嵌入式服务端（一体模式）")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "mihomo-tui — mihomo 终端 UI 配置工具")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "用法:")
		fmt.Fprintln(os.Stderr, "  mihomo-tui [选项]              启动 TUI 客户端")
		fmt.Fprintln(os.Stderr, "  mihomo-tui server [选项]       启动后台 IPC 服务")
		fmt.Fprintln(os.Stderr, "  mihomo-tui install_service     安装为 systemd 服务（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui uninstall           卸载 systemd 服务（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui cleanup             清理系统代理和 TUN 环境（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui version             显示版本信息")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "选项:")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()

	// 无子命令：启动 TUI
	if len(args) == 0 {
		cmd.RunTUI(*dir, *standalone)
		return
	}

	// 子命令分发
	switch args[0] {
	case "server":
		cmd.RunServer(args[1:], *dir)
	case "install_service":
		cmd.RunInstallService(*dir)
	case "uninstall":
		cmd.RunUninstallService()
	case "cleanup":
		cmd.RunCleanup()
	case "version":
		cmd.RunVersion()
	case "help":
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", args[0])
		flag.Usage()
		os.Exit(1)
	}
}
