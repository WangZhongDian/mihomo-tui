package ui

import (
	"fmt"
	"os"
	"os/exec"
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

	app := tview.NewApplication().EnableMouse(true)

	// 先创建所有页面，用于后续键盘导航判断
	homePage, _ := NewDashboard(app)
	proxyPage := NewProxyPage(app)
	subPage := NewSubscriptionPage(app)
	connPage := NewConnectionsPage(app)
	rulesPage := NewRulesPage(app)
	logsPage := NewLogsPage(app)
	settingsPage := NewSettingsPage(app)

	// 页面管理器
	pages := tview.NewPages()
	// 初始化 setFocus，避免嵌套 Pages 导致 HasFocus 误判后 nil pointer panic
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	pages.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	pages.AddPage("home", homePage, true, true)
	pages.AddPage("proxy", proxyPage, true, false)
	pages.AddPage("subscription", subPage, true, false)
	pages.AddPage("connections", connPage, true, false)
	pages.AddPage("rules", rulesPage, true, false)
	pages.AddPage("logs", logsPage, true, false)
	pages.AddPage("settings", settingsPage, true, false)

	// 页面映射，用于生命周期管理
	pageMap := map[string]tview.Primitive{
		"home":         homePage,
		"proxy":        proxyPage,
		"subscription": subPage,
		"connections":  connPage,
		"rules":        rulesPage,
		"logs":         logsPage,
		"settings":     settingsPage,
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
		}
	}

	// 左侧导航栏
	navList := tview.NewList().
		AddItem(" 首页", "", 0, switchPage("home")).
		AddItem(" 代理", "", 0, switchPage("proxy")).
		AddItem(" 订阅", "", 0, switchPage("subscription")).
		AddItem(" 连接", "", 0, switchPage("connections")).
		AddItem(" 规则", "", 0, switchPage("rules")).
		AddItem(" 日志", "", 0, switchPage("logs")).
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

	err := app.SetRoot(mainLayout, true).SetFocus(navList).Run()

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
	// 检查服务端是否已运行
	if mihomotui.IPCCheckDaemon() {
		fmt.Println("[mihomo-tui] 已连接到独立服务端（分体模式）")
		return nil
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
	fmt.Println("[mihomo-tui] 已启动嵌入式服务端（一体模式）")
	return nil
}
