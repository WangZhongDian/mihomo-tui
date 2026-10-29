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
	"mihomotui/mihomotui/i18n"
)

var embeddedDaemonStarted bool

// Run 启动 TUI 应用主循环。
func Run(standalone bool) error {
	if err := ensureDaemon(standalone); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.Tf("startup.connect_error", map[string]any{"Error": err.Error()}))
		os.Exit(1)
	}
	if err := syncConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", i18n.Tf("startup.sync_error", map[string]any{"Error": err.Error()}))
		os.Exit(1)
	}
	if changed, skipped, err := mihomotui.ReconcileSystemProxyEnvIfUsable(); err != nil {
		mihomotui.Warnf("系统代理环境变量对账失败: %v", err)
	} else if changed {
		if skipped {
			mihomotui.Infof("已按偏好清理系统代理环境变量注入")
		} else {
			mihomotui.Infof("已按偏好重建系统代理环境变量注入")
		}
	}

	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)
	root := tview.NewPages()
	root.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	var mainLayout tview.Primitive
	startMain := func() {
		i18n.SetLanguage(mihomotui.EffectiveLanguage())
		if mainLayout == nil {
			mainLayout = newMainLayout(app)
			root.AddPage("main", mainLayout, true, true)
		}
		app.SetFocus(root)
	}
	if mihomotui.NeedsLanguageSetup() {
		root.AddPage("language", newLanguageSetupPage(app, startMain), true, true)
	} else {
		startMain()
	}

	// 统一处理信号退出，避免终端停留在备用屏幕和鼠标上报状态。
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
			time.AfterFunc(2*time.Second, func() {
				terminalRestore()
				os.Exit(0)
			})
		case <-sigStop:
		}
	}()

	err := app.SetRoot(root, true).Run()
	terminalRestore()
	if embeddedDaemonStarted {
		if client, cerr := mihomotui.GetIPCClient(); cerr == nil {
			_ = client.IPCShutdownDaemon()
		}
	}
	return err
}

func newMainLayout(app *tview.Application) tview.Primitive {
	homePage, _ := NewDashboard(app)
	proxyPage := NewProxyPage(app)
	subPage := NewSubscriptionPage(app)
	poolPage := NewSubscriptionPoolPage(app)
	connPage := NewConnectionsPage(app)
	rulesPage := NewRulesPage(app)
	logsPage := NewLogsPage(app)
	resourcePage := NewResourcePage(app)
	settingsPage := NewSettingsPage(app)

	pages := tview.NewPages()
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

	pageMap := map[string]tview.Primitive{
		"home": homePage, "proxy": proxyPage, "subscription": subPage,
		"subscription-pools": poolPage, "connections": connPage,
		"rules": rulesPage, "logs": logsPage, "resources": resourcePage, "settings": settingsPage,
	}
	var currentPage string
	switchPage := func(page string) func() {
		return func() {
			if currentPage != "" && currentPage != page {
				if p, ok := pageMap[currentPage].(Page); ok {
					p.Stop()
				}
			}
			currentPage = page
			pages.SwitchToPage(page)
			if p, ok := pageMap[page].(Shower); ok {
				p.OnShow()
			}
		}
	}

	navList := tview.NewList().
		AddItem(" "+i18n.T("nav.home"), "", 0, switchPage("home")).
		AddItem(" "+i18n.T("nav.proxy"), "", 0, switchPage("proxy")).
		AddItem(" "+i18n.T("nav.subscription"), "", 0, switchPage("subscription")).
		AddItem(" "+i18n.T("nav.pools"), "", 0, switchPage("subscription-pools")).
		AddItem(" "+i18n.T("nav.connections"), "", 0, switchPage("connections")).
		AddItem(" "+i18n.T("nav.rules"), "", 0, switchPage("rules")).
		AddItem(" "+i18n.T("nav.logs"), "", 0, switchPage("logs")).
		AddItem(" "+i18n.T("nav.resources"), "", 0, switchPage("resources")).
		AddItem(" "+i18n.T("nav.settings"), "", 0, switchPage("settings"))
	navList.SetBorder(true).SetTitle(" " + i18n.T("nav.menu") + " ").SetTitleAlign(tview.AlignCenter)
	navList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(pages)
			return nil
		}
		return event
	})

	mainLayout := tview.NewFlex().AddItem(navList, 22, 1, true).AddItem(pages, 0, 4, true)
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc && app.GetFocus() != navList {
			app.SetFocus(navList)
			return nil
		}
		return event
	})
	app.SetFocus(navList)
	return mainLayout
}

func newLanguageSetupPage(app *tview.Application, done func()) tview.Primitive {
	if mihomotui.DetectSystemLanguage() == i18n.SimplifiedChinese {
		i18n.SetLanguage(i18n.SimplifiedChinese)
	} else {
		i18n.SetLanguage(i18n.English)
	}
	pages := tview.NewPages()
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	status := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	form := tview.NewForm()
	selected := 0
	if i18n.Language() == i18n.English {
		selected = 1
	}
	form.AddDropDown(i18n.T("startup.select_language"), []string{"简体中文", "English"}, selected, nil).
		AddButton(i18n.T("common.confirm"), func() {
			index, _ := form.GetFormItem(0).(*tview.DropDown).GetCurrentOption()
			language := i18n.SimplifiedChinese
			if index == 1 {
				language = i18n.English
			}
			if err := mihomotui.SetLanguagePreference(language); err != nil {
				status.SetText("[red]" + SanitizeDisplayText(i18n.Tf("startup.save_language_failed", map[string]any{"Error": err.Error()})) + "[-]")
				return
			}
			done()
		})
	form.SetBorder(true).SetTitle(" " + i18n.T("startup.language_prompt") + " ")
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(form, 5, 0, true).
		AddItem(tview.NewTextView().SetText(i18n.T("startup.language_hint")).SetTextAlign(tview.AlignCenter), 2, 0, false).
		AddItem(status, 1, 0, false).
		AddItem(nil, 0, 1, false)
	centered := tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(layout, 76, 0, true).AddItem(nil, 0, 1, false)
	pages.AddPage("language", centered, true, true)
	return pages
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
	local := mihomotui.GlobalConfig()
	localMihomoConfigPath := local.MihomoConfigPath
	localMihomoBinaryPath := local.MihomoBinaryPath
	localLogDir := local.LogDir
	mihomotui.SetGlobalConfig(*cfg)
	restored := mihomotui.GlobalConfig()
	restored.MihomoConfigPath = localMihomoConfigPath
	restored.MihomoBinaryPath = localMihomoBinaryPath
	restored.LogDir = localLogDir
	mihomotui.SetGlobalConfig(*restored)
	_ = mihomotui.InitLogger(restored.LogDir, restored.LogLevel)
	mihomotui.ResetMihomoAPI()
	return nil
}

// ensureDaemon 确保 IPC 服务端已启动；standalone 允许自动启动嵌入式服务端。
func ensureDaemon(standalone bool) error {
	if err := mihomotui.IPCProbeDaemon(); err == nil {
		mihomotui.Infof("已连接到独立服务端（分体模式）")
		return nil
	} else if mihomotui.IsIPCPermissionError(err) {
		return err
	}
	if !standalone {
		return fmt.Errorf("未检测到 IPC 服务端，请先运行: mihomo-tui server\n或添加 --standalone 参数启动一体模式")
	}
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	args := []string{"server"}
	if customDir := mihomotui.GetCustomConfigDir(); customDir != "" {
		args = append(args, "-d", customDir)
	}
	cmd := exec.Command(execPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), "MIHOMO_TUI_LAUNCH_MODE=embedded")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动服务端失败: %w", err)
	}
	if err := mihomotui.IPCWaitForDaemon(5 * time.Second); err != nil {
		return fmt.Errorf("等待服务端就绪失败: %w", err)
	}
	embeddedDaemonStarted = true
	mihomotui.Infof("已启动嵌入式服务端（一体模式）")
	return nil
}
