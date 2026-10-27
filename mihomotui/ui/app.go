package ui

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

var (
	embeddedDaemonStarted bool
)

// Run 启动 TUI 应用主循环
// standalone 为 true 时，若未检测到服务端则自动启动嵌入式服务端（一体模式）
func Run(standalone bool) error {
	// 确保 IPC 服务端已启动
	if err := ensureDaemon(standalone); err != nil {
		fmt.Fprintf(os.Stderr, "无法连接 IPC 服务: %v\n", err)
		os.Exit(1)
	}

	// 从服务端同步配置
	if err := syncConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "同步配置失败: %v\n", err)
		os.Exit(1)
	}

	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)

	// 先创建所有页面，用于后续键盘导航判断
	homePage, _ := NewDashboard(app)
	proxyPage := NewProxyPage(app)
	subPage := NewSubscriptionPage(app)
	poolPage := NewSubscriptionPoolPage(app)
	connPage := NewConnectionsPage(app)
	rulesPage := NewRulesPage(app)
	logsPage := NewLogsPage(app)
	resourcePage := NewResourcePage(app)
	settingsPage := NewSettingsPage(app)

	// 页面管理器
	pages := tview.NewPages()
	// 初始化 setFocus，避免嵌套 Pages 导致 HasFocus 误判后 nil pointer panic
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	pages.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	pages.AddPage("home", homePage, true, true)
	pages.AddPage("proxy", proxyPage, true, false)
	pages.AddPage("subscription", subPage, true, false)
	pages.AddPage("subscription-pools", poolPage, true, false)
	pages.AddPage("connections", connPage, true, false)
	pages.AddPage("rules", rulesPage, true, false)
	pages.AddPage("logs", logsPage, true, false)
	pages.AddPage("resources", resourcePage, true, false)
	pages.AddPage("settings", settingsPage, true, false)

	// 页面映射，用于生命周期管理
	pageMap := map[string]tview.Primitive{
		"home":               homePage,
		"proxy":              proxyPage,
		"subscription":       subPage,
		"subscription-pools": poolPage,
		"connections":        connPage,
		"rules":              rulesPage,
		"logs":               logsPage,
		"resources":          resourcePage,
		"settings":           settingsPage,
	}
	var currentPage string

	// 导航切换函数
	switchPage := func(page string) func() {
		return func() {
			// 切换前停止当前页面的后台 goroutine
			if currentPage != "" && currentPage != page {
				if p, ok := pageMap[currentPage].(Page); ok {
					p.Stop()
				}
			}
			currentPage = page
			pages.SwitchToPage(page)
			// 切换到可见时刷新数据（如订阅池需要感知订阅删除）。
			if p, ok := pageMap[page].(Shower); ok {
				p.OnShow()
			}
		}
	}

	// 左侧导航栏
	navList := tview.NewList().
		AddItem(" 首页", "", 0, switchPage("home")).
		AddItem(" 代理", "", 0, switchPage("proxy")).
		AddItem(" 订阅", "", 0, switchPage("subscription")).
		AddItem(" 订阅池", "", 0, switchPage("subscription-pools")).
		AddItem(" 连接", "", 0, switchPage("connections")).
		AddItem(" 规则", "", 0, switchPage("rules")).
		AddItem(" 日志", "", 0, switchPage("logs")).
		AddItem(" 资源管理", "", 0, switchPage("resources")).
		AddItem(" 设置", "", 0, switchPage("settings"))
	navList.SetBorder(true).
		SetTitle(" 菜单 ").
		SetTitleAlign(tview.AlignCenter)

	// 导航栏 Tab 键进入右侧页面（覆盖 List 默认将 Tab 当作 Down 的行为）
	navList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(pages)
			return nil
		}
		return event
	})

	// 主布局：左右分栏
	mainLayout := tview.NewFlex().
		AddItem(navList, 22, 1, true).
		AddItem(pages, 0, 4, true)

	// 全局键盘捕获
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		focus := app.GetFocus()
		switch event.Key() {
		case tcell.KeyEsc:
			// Esc 返回导航栏
			if focus != navList {
				app.SetFocus(navList)
				return nil
			}
		}
		return event
	})

	// 信号退出：Ctrl+C 在 raw 模式下是按键事件，由 tview 内部转成 Stop()；
	// 但进程也可能收到真正的 SIGINT/SIGTERM/SIGHUP（终端被关闭、被 systemd
	// 停止、被 kill、或终端未进入 raw 模式）。没有处理函数时进程会在绘制中途
	// 直接消失，终端停在「备用屏幕 + 鼠标上报 + 隐藏光标」状态，
	// 之后每一次滚轮/按键都会被 shell 当成普通文本回显，表现为大量残留输出。
	// 这里统一转成 app.Stop()，让 tview 走完 Fini() 的正常退出路径。
	var (
		sigCh   = make(chan os.Signal, 1)
		sigStop = make(chan struct{})
	)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer func() {
		signal.Stop(sigCh)
		close(sigStop)
	}()
	go func() {
		select {
		case sig, ok := <-sigCh:
			if !ok {
				return
			}
			mihomotui.Infof("收到退出信号 %s，正在还原终端并退出", sig)
			app.Stop()
			// 兜底：如果 Fini() 因为某个 goroutine 卡住而没能完成，
			// 超时后强制还原终端再退出——绝不能让用户停留在
			// 「备用屏幕 + 鼠标上报」状态里。
			time.AfterFunc(2*time.Second, func() {
				terminalRestore()
				os.Exit(0)
			})
		case <-sigStop:
		}
	}()

	err := app.SetRoot(mainLayout, true).SetFocus(navList).Run()

	// 无论 Run() 如何返回（正常退出、Ctrl+C、收到 SIGTERM/SIGHUP），
	// 都把终端从「备用屏幕 + 鼠标上报 + 隐藏光标」状态还原回来。
	// tcell 的 Fini() 通常已经处理过，这里是幂等兜底：进程被信号打断、
	// 或 Fini 未覆盖到的路径都能靠它收尾，避免终端残留画面和鼠标转义序列。
	terminalRestore()

	// TUI 退出时，如果是本进程启动的 embedded daemon，停止它
	if embeddedDaemonStarted {
		if client, cerr := mihomotui.GetIPCClient(); cerr == nil {
			_ = client.IPCShutdownDaemon()
		}
	}

	return err
}

// syncConfig 从服务端同步配置到本地（保留本地路径字段，避免跨用户绝对路径污染）
func syncConfig() error {
	client, err := mihomotui.GetIPCClient()
	if err != nil {
		return fmt.Errorf("创建 IPC 客户端失败: %w", err)
	}
	cfg, err := client.IPCGetConfig()
	if err != nil {
		return fmt.Errorf("获取配置失败: %w", err)
	}

	// 保存本地路径字段（服务端和客户端的绝对路径不共享）
	local := mihomotui.GlobalConfig()
	localMihomoConfigPath := local.MihomoConfigPath
	localMihomoBinaryPath := local.MihomoBinaryPath
	localLogDir := local.LogDir

	mihomotui.SetGlobalConfig(*cfg)

	// 恢复本地路径字段
	restored := mihomotui.GlobalConfig()
	restored.MihomoConfigPath = localMihomoConfigPath
	restored.MihomoBinaryPath = localMihomoBinaryPath
	restored.LogDir = localLogDir
	mihomotui.SetGlobalConfig(*restored)

	// 重新初始化日志，确保日志目录使用本地用户路径（避免 syncConfigDir 切换到服务端目录）
	_ = mihomotui.InitLogger(restored.LogDir, restored.LogLevel)

	mihomotui.ResetMihomoAPI()
	return nil
}

// ensureDaemon 确保 IPC 守护进程已启动
// standalone 为 true 时，若服务端未运行则自动启动嵌入式服务端
func ensureDaemon(standalone bool) error {
	// 检查服务端是否已运行；权限不足必须直接提示，不能误报为服务未运行。
	// 注意：这里不向 stdout 打印任何内容。TUI 启动后 tcell 会接管终端并进入
	// 备用屏幕，退出时恢复原画面——启动前打印的一行会一直留在屏幕上，
	// 成为「退出后残留输出」的一部分。运行模式只写入日志文件。
	if err := mihomotui.IPCProbeDaemon(); err == nil {
		mihomotui.Infof("已连接到独立服务端（分体模式）")
		return nil
	} else if mihomotui.IsIPCPermissionError(err) {
		return err
	}

	if !standalone {
		return fmt.Errorf("未检测到 IPC 服务端，请先运行: mihomo-tui server\n或添加 --standalone 参数启动一体模式")
	}

	// 获取当前可执行文件路径
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	args := []string{"server"}
	// 如果 TUI 指定了自定义目录，传递给 embedded server
	if customDir := mihomotui.GetCustomConfigDir(); customDir != "" {
		args = append(args, "-d", customDir)
	}

	// 在后台启动服务端（标记为嵌入模式）
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), "MIHOMO_TUI_LAUNCH_MODE=embedded")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动服务端失败: %w", err)
	}

	// 等待服务端就绪
	if err := mihomotui.IPCWaitForDaemon(5 * time.Second); err != nil {
		return fmt.Errorf("等待服务端就绪失败: %w", err)
	}

	embeddedDaemonStarted = true
	mihomotui.Infof("已启动嵌入式服务端（一体模式）")
	return nil
}
