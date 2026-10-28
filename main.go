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
		fmt.Fprintln(os.Stderr, "  mihomo-tui start               启动 mihomo 内核（需 IPC 服务；root 守护进程下需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui stop                停止 mihomo 内核（需 IPC 服务；root 守护进程下需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui restart             重启 mihomo 内核（需 IPC 服务；root 守护进程下需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui status              查询 mihomo 内核状态（需 IPC 服务）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui subscription import --url/--file/--stdin  导入订阅")
		fmt.Fprintln(os.Stderr, "  mihomo-tui install_service [--kernel-was-running=true|false]")
		fmt.Fprintln(os.Stderr, "                              安装为 systemd 服务（需 root）；可按安装前内核状态恢复启动")
		fmt.Fprintln(os.Stderr, "  mihomo-tui uninstall           卸载 systemd 服务（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui grant_operator 用户  授予普通用户订阅管理权限（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui cleanup [--tun-only]     清理系统代理和 TUN 环境（需 root；--tun-only 仅清 TUN 规则）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui tun_diagnose        输出 TUN 路由 dry-run 计划（不修改系统）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui tun_debug [--apply] 输出 TUN 预检；--apply 时重建修复并输出日志（需 root）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui doctor              诊断系统代理注入状态（只读，不改配置）")
		fmt.Fprintln(os.Stderr, "  mihomo-tui unsetshell          输出清除当前终端代理变量的 shell 代码")
		fmt.Fprintln(os.Stderr, "                                用法: eval \"$(mihomo-tui unsetshell)\"")
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
	case "start":
		cmd.RunMihomoStart()
	case "stop":
		cmd.RunMihomoStop()
	case "restart":
		cmd.RunMihomoRestart()
	case "status":
		cmd.RunMihomoStatus()
	case "subscription":
		if len(args) < 2 || args[1] != "import" {
			fmt.Fprintln(os.Stderr, "用法: mihomo-tui subscription import --url/--file/--stdin")
			os.Exit(1)
		}
		cmd.RunSubscriptionImport(args[2:], *dir)
	case "install_service":
		cmd.RunInstallService(*dir, args[1:])
	case "uninstall":
		cmd.RunUninstallService()
	case "grant_operator":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "用法: mihomo-tui grant_operator 用户名")
			os.Exit(1)
		}
		cmd.RunGrantOperator(args[1])
	case "cleanup":
		cmd.RunCleanup(args[1:])
	case "tun_diagnose":
		cmd.RunTUNDiagnose()
	case "tun_debug":
		cmd.RunTUNDebug(*dir, args[1:])
	case "doctor":
		cmd.RunDoctor()
	case "unsetshell":
		cmd.RunUnsetShellEnv()
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
