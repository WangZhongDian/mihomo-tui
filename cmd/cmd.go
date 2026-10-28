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

// RunInstallService 安装 systemd 服务。
// args 可含 --kernel-was-running=true|false，供 install.sh 在 stop 前传入快照。
func RunInstallService(dir string, args []string) {
	if dir != "" {
		mihomotui.SetCustomConfigDir(dir)
	}
	fs := flag.NewFlagSet("install_service", flag.ExitOnError)
	kernelHint := fs.String("kernel-was-running", "", "安装前 mihomo 内核是否在运行 (true/false)；省略则自动探测或读环境变量")
	_ = fs.Parse(args)
	if err := InstallServiceWithKernelHint(*kernelHint); err != nil {
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

// RunDoctor 输出系统代理注入的逐项诊断，帮助定位"开了但没生效"。
// 只读：不修改任何文件或环境变量。
func RunDoctor() {
	diag := mihomotui.DiagnoseSystemProxy()
	fmt.Println("mihomo-tui 系统代理诊断（只读，不修改任何配置）")
	fmt.Println()
	fmt.Printf("配置目录      : %s\n", diag.ConfigDir)
	fmt.Printf("当前用户      : uid=%d  home=%s\n", os.Geteuid(), mihomotui.SystemProxyTargetHomeForDisplay())
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		fmt.Printf("SUDO_USER     : %s（注入目标为发起 sudo 的用户）\n", sudoUser)
	}
	fmt.Printf("偏好开关      : %s\n", map[bool]string{true: "已开启", false: "已关闭"}[diag.PreferenceEnabled])
	fmt.Printf("代理端口      : http=127.0.0.1:%d socks=127.0.0.1:%d\n", diag.HTTPPort, diag.SocksPort)
	fmt.Printf("端口监听      : %s\n", map[bool]string{true: "是", false: "否 ← 内核未启动时走代理会失败"}[diag.PortListening])
	fmt.Println()
	fmt.Println("shell 配置文件:")
	for _, f := range diag.ShellFiles {
		state := "未注入"
		switch {
		case !f.Exists:
			state = "文件不存在"
		case f.HasBlk && f.HasHTTP:
			state = "已注入（端口匹配）"
		case f.HasBlk:
			state = "已注入（端口不匹配！）"
		}
		marker := " "
		if f.LoginEntry {
			marker = "*"
		}
		fmt.Printf("  %s %-40s %s\n", marker, f.Path, state)
	}
	fmt.Println("  * = 登录 shell 的实际入口（bash 按 .bash_profile → .bash_login → .profile 取第一个）")
	if diag.UnitExecStop != "" {
		fmt.Printf("\nsystemd unit ExecStop: %s\n", diag.UnitExecStop)
	}
	fmt.Println()
	fmt.Println("当前进程环境变量:")
	for _, v := range diag.SessionVars {
		if v.Set {
			fmt.Printf("  %-12s = %s\n", v.Name, v.Value)
		} else {
			fmt.Printf("  %-12s = (未设置)\n", v.Name)
		}
	}
	if len(diag.Notes) > 0 {
		fmt.Println()
		fmt.Println("诊断结论:")
		for _, note := range diag.Notes {
			fmt.Println("  ! " + note)
		}
	}
}

// RunUnsetShellEnv 输出一段 shell 代码，用于清除当前终端里残留的代理环境变量。
//
// 为什么需要它：进程无法修改其他已运行 shell 的环境变量（Unix 限制），
// 而注入块被删除后 `source ~/.bashrc` 也不会 unset 已有变量。
// 这里的做法是把 unset 语句打印出来，由用户的 shell 自己 eval 执行。
//
// 用法: eval "$(mihomo-tui unsetshell)"
func RunUnsetShellEnv() {
	fmt.Println("# 用法: eval \"$(mihomo-tui unsetshell)\"")
	fmt.Println("unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy")
	fmt.Println("# no_proxy / NO_PROXY 保留：本机回环不走代理，清掉反而可能让本地服务被代理劫持")
}

// RunVersion 输出版本信息
func RunVersion() {
	fmt.Printf("mihomo-tui %s\n", mihomotui.Version)
}

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

// RunTUNDebug 输出 TUN 预检和拟执行命令；只有 --apply 才会真实修改网络状态。
func RunTUNDebug(dir string, args []string) {
	flags := flag.NewFlagSet("tun_debug", flag.ExitOnError)
	apply := flags.Bool("apply", false, "执行 TUN 路由修复（默认仅只读诊断）")
	flags.Parse(args)
	if *apply && os.Geteuid() != 0 {
		fmt.Println("TUN 路由修复应用需要 root 权限，请使用 sudo mihomo-tui tun_debug --apply")
		os.Exit(1)
	}
	if *apply {
		if dir != "" {
			mihomotui.SetCustomConfigDir(dir)
		} else if mihomotui.GetCustomConfigDir() == "" {
			mihomotui.SetCustomConfigDir("/var/lib/mihomo-tui")
		}
	}
	if err := mihomotui.InitConsoleLogger(os.Stdout, "debug"); err != nil {
		fmt.Printf("初始化 stdout 调试日志失败: %v\n", err)
		os.Exit(1)
	}
	if *apply {
		fmt.Println("mihomo-tui TUN 路由调试（将实际修改项目路由和防火墙）：")
	} else {
		fmt.Println("mihomo-tui TUN 路由调试（只读；未修改系统网络）：")
	}
	if err := mihomotui.DebugTUNRouting(os.Stdout, *apply); err != nil {
		fmt.Printf("TUN 路由调试失败: %v\n", err)
		os.Exit(1)
	}
	if *apply {
		fmt.Println("✅ TUN 路由修复调试完成，规则保持启用状态")
	}
}

// RunCleanup 清理环境。默认同时清理系统代理环境变量和 TUN 路由规则；
// --tun-only 只清理 TUN 路由规则（systemd ExecStop 使用，不动用户 shell 配置）。
func RunCleanup(args []string) {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	tunOnly := fs.Bool("tun-only", false, "只清理 TUN 路由规则，保留用户 shell 中的系统代理环境变量")
	_ = fs.Parse(args)

	if os.Geteuid() == 0 && mihomotui.GetCustomConfigDir() == "" {
		mihomotui.SetCustomConfigDir("/var/lib/mihomo-tui")
	}
	if *tunOnly {
		mihomotui.CleanupTUNRoutingOnly()
		fmt.Println("✅ TUN 路由规则清理完成（系统代理环境变量保留）")
		return
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

// ipcClientOrExit 连接已运行的 IPC 守护进程；失败时打印提示并退出。
func ipcClientOrExit() *mihomotui.IPCClient {
	if err := mihomotui.IPCProbeDaemon(); err != nil {
		if mihomotui.IsIPCPermissionError(err) {
			fmt.Fprintf(os.Stderr, "连接 IPC 服务失败: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "连接 IPC 服务失败: %v\n请先启动守护进程：sudo systemctl start mihomo-tui  或  mihomo-tui server\n", err)
		}
		os.Exit(1)
	}
	client, err := mihomotui.GetIPCClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 IPC 服务失败: %v\n", err)
		os.Exit(1)
	}
	return client
}

func printMihomoStatus(status *mihomotui.MihomoStatusResponse) {
	if status == nil {
		fmt.Println("内核状态: 未知")
		return
	}
	if status.Running {
		fmt.Println("内核状态: 运行中")
		if status.PID > 0 {
			fmt.Printf("PID: %d\n", status.PID)
		}
		if status.RunningVersion != "" {
			fmt.Printf("版本: %s\n", status.RunningVersion)
		}
		if status.VersionAt != "" {
			fmt.Printf("版本记录时间: %s\n", status.VersionAt)
		}
		return
	}
	fmt.Println("内核状态: 已停止")
}

// RunMihomoStart 通过 IPC 启动 mihomo 内核。
// root 守护进程下该操作需要 root 权限（与 TUI/IPC 鉴权一致）。
func RunMihomoStart() {
	client := ipcClientOrExit()
	if err := client.IPCStartMihomo(); err != nil {
		fmt.Fprintf(os.Stderr, "启动内核失败: %v\n", err)
		if mihomotui.IsIPCPermissionError(err) {
			fmt.Fprintln(os.Stderr, "提示: root 守护进程下请使用 sudo mihomo-tui start")
		}
		os.Exit(1)
	}
	fmt.Println("✅ 内核已启动")
	// 内核起来后端口已监听，按偏好对账：偏好为开启时补齐注入块。
	// 端口尚未就绪时 ReconcileSystemProxyEnvIfUsable 会跳过，避免注入死端口。
	if _, skipped, rerr := mihomotui.ReconcileSystemProxyEnvIfUsable(); rerr != nil {
		mihomotui.Warnf("启动后对账系统代理环境变量失败: %v", rerr)
	} else if skipped {
		fmt.Println("⚠️  内核已启动但代理端口尚未监听，暂未注入系统代理；稍后可重新开启")
	}
	if status, err := client.IPCGetMihomoStatus(); err == nil {
		printMihomoStatus(status)
	}
}

// RunMihomoStop 通过 IPC 停止 mihomo 内核。
// 停内核后代理端口不再监听，因此同步关闭系统代理，避免新开终端拿到指向死端口的变量。
func RunMihomoStop() {
	client := ipcClientOrExit()
	if err := client.IPCStopMihomo(); err != nil {
		fmt.Fprintf(os.Stderr, "停止内核失败: %v\n", err)
		if mihomotui.IsIPCPermissionError(err) {
			fmt.Fprintln(os.Stderr, "提示: root 守护进程下请使用 sudo mihomo-tui stop")
		}
		os.Exit(1)
	}
	fmt.Println("✅ 内核已停止")

	// 系统代理是本用户的本地偏好，必须在客户端（而非 root daemon）侧关闭。
	disabled, derr := mihomotui.DisableSystemProxyOnKernelStop()
	if derr != nil {
		fmt.Fprintf(os.Stderr, "⚠️  自动关闭系统代理失败: %v\n", derr)
		fmt.Fprintln(os.Stderr, "   请手动关闭，或执行 mihomo-tui cleanup")
	} else if disabled {
		fmt.Println("✅ 已自动关闭系统代理（避免新终端指向已停止的代理端口）")
		fmt.Println(mihomotui.SystemProxyClearHint())
	}
}

// RunMihomoRestart 通过 IPC 重启 mihomo 内核；若当前未运行则直接启动。
func RunMihomoRestart() {
	client := ipcClientOrExit()
	if err := client.IPCRestartMihomo(); err != nil {
		fmt.Fprintf(os.Stderr, "重启内核失败: %v\n", err)
		if mihomotui.IsIPCPermissionError(err) {
			fmt.Fprintln(os.Stderr, "提示: root 守护进程下请使用 sudo mihomo-tui restart")
		}
		os.Exit(1)
	}
	fmt.Println("✅ 内核已重启")
	// 内核重启后端口重新监听，按偏好对账一次：若此前因停内核被关闭则保持关闭，
	// 若仍开启则确保注入块端口与当前配置一致。端口未就绪时自动跳过。
	if _, _, rerr := mihomotui.ReconcileSystemProxyEnvIfUsable(); rerr != nil {
		mihomotui.Warnf("重启后对账系统代理环境变量失败: %v", rerr)
	}
	if status, err := client.IPCGetMihomoStatus(); err == nil {
		printMihomoStatus(status)
	}
}

// RunMihomoStatus 查询 mihomo 内核运行状态。
func RunMihomoStatus() {
	client := ipcClientOrExit()
	status, err := client.IPCGetMihomoStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "查询内核状态失败: %v\n", err)
		os.Exit(1)
	}
	printMihomoStatus(status)
	if status != nil && !status.Running {
		os.Exit(3)
	}
}
