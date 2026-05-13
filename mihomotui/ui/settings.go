package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// buildProgressBar 生成文本进度条，percent 为 0-100
func buildProgressBar(percent int) string {
	width := 20
	filled := min(percent*width/100, width)
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

// NewSettingsPage 创建设置页面
func NewSettingsPage(app *tview.Application) tview.Primitive {
	activeTab := 0 // 0=系统设置, 1=mihomo设置, 2=关于

	// Tab 按钮
	tabSystem := tview.NewButton(" 系统设置 ")
	tabSystem.SetBorder(false)
	tabMihomo := tview.NewButton(" mihomo 设置 ")
	tabMihomo.SetBorder(false)
	tabAbout := tview.NewButton(" 关于 ")
	tabAbout.SetBorder(false)

	// Tab 栏
	tabs := tview.NewFlex().
		AddItem(tabSystem, 0, 1, true).
		AddItem(tabMihomo, 0, 1, true).
		AddItem(tabAbout, 0, 1, true)

	// 内容区域
	content := tview.NewFlex()

	// Pages 容器，用于支持弹窗覆盖
	settingsPages := tview.NewPages()
	settingsPages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	// 弹窗显示（提前定义供表单保存回调使用）
	showModal := func(title, message string) {
		modal := tview.NewModal().
			SetText(fmt.Sprintf("%s\n\n%s", title, message)).
			AddButtons([]string{"确认"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				settingsPages.HidePage("modal")
				settingsPages.RemovePage("modal")
			})
		settingsPages.AddPage("modal", modal, true, true)
	}

	cfg := mihomotui.GlobalConfig()

	// 统一保存函数：异步 IPC 同步到服务端
	doSave := func(fieldName string) {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				mihomotui.Warnf("保存 %s 失败: %v", fieldName, err)
				return
			}
			if err := client.IPCUpdateConfig(cfg); err != nil {
				mihomotui.Warnf("保存 %s 失败: %v", fieldName, err)
				return
			}
			_ = mihomotui.InitLogger(cfg.LogDir, cfg.LogLevel)
			mihomotui.Infof("设置 %s 已保存", fieldName)
		}()
	}

	// 语言索引映射
	langIdx := 0
	if cfg.System.Language == "en" {
		langIdx = 1
	}
	// mihomo 日志级别索引映射
	logLevels := []string{"debug", "info", "warning", "error", "silent"}
	logIdx := 1
	for i, v := range logLevels {
		if v == cfg.Mihomo.LogLevel {
			logIdx = i
			break
		}
	}
	// app 日志级别索引映射
	appLogLevels := []string{"debug", "info", "warn", "error"}
	appLogIdx := 1
	for i, v := range appLogLevels {
		if v == cfg.LogLevel {
			appLogIdx = i
			break
		}
	}

	// 通过 IPC 从服务端获取工作目录
	workDir := mihomotui.GetConfigDir()
	if client, err := mihomotui.GetIPCClient(); err == nil {
		if dir, err := client.IPCGetConfigDir(); err == nil && dir != "" {
			workDir = dir
		} else {
			mihomotui.Errorf("获取工作目录失败: %v", err)
		}
	}

	// ---------- 系统设置 ----------
	systemForm := tview.NewForm()
	systemForm.AddCheckbox("开机启动", cfg.System.AutoStart, func(checked bool) {
		cfg.System.AutoStart = checked
		doSave("开机启动")
	}).
		AddCheckbox("系统代理", cfg.System.SystemProxy, func(checked bool) {
			cfg.System.SystemProxy = checked
			if err := cfg.SetSystemProxyEnv(checked); err != nil {
				mihomotui.Warnf("系统代理环境变量设置失败: %v", err)
			}
			doSave("系统代理")
		}).
		AddCheckbox("虚拟网卡模式", cfg.System.TUN, func(checked bool) {
			cfg.System.TUN = checked
			doSave("虚拟网卡模式")
		}).
		AddDropDown("语言", []string{"简体中文", "English"}, langIdx, func(option string, optionIndex int) {
			if optionIndex == 1 {
				cfg.System.Language = "en"
			} else {
				cfg.System.Language = "zh-CN"
			}
			doSave("语言")
		}).
		AddDropDown("应用日志级别", []string{"DEBUG", "INFO", "WARN", "ERROR"}, appLogIdx, func(option string, optionIndex int) {
			cfg.LogLevel = appLogLevels[optionIndex]
			doSave("应用日志级别")
		}).
		AddInputField("日志目录", cfg.LogDir, 50, nil, func(text string) {
			cfg.LogDir = text
		}).
		AddInputField("工作目录", workDir, 50, func(text string, ch rune) bool { return false }, nil)

	// 为 systemForm 的每个 InputField 设置 blur 保存
	for i := 0; i < systemForm.GetFormItemCount(); i++ {
		item := systemForm.GetFormItem(i)
		if inputField, ok := item.(*tview.InputField); ok {
			label := inputField.GetLabel()
			if label == "工作目录" {
				continue // 只读字段，跳过
			}
			fieldLabel := label
			inputField.SetFinishedFunc(func(key tcell.Key) {
				doSave(fieldLabel)
			})
		}
	}
	systemForm.SetBorder(true).SetTitle(" 系统设置 ")

	// ---------- mihomo 设置 ----------
	mihomoForm := tview.NewForm()
	mihomoForm.AddInputField("HTTP 端口", strconv.Itoa(cfg.Mihomo.HTTPPort), 10, nil, func(text string) {
		if v, err := strconv.Atoi(text); err == nil {
			cfg.Mihomo.HTTPPort = v
		}
	}).
		AddInputField("SOCKS5 端口", strconv.Itoa(cfg.Mihomo.SOCKS5Port), 10, nil, func(text string) {
			if v, err := strconv.Atoi(text); err == nil {
				cfg.Mihomo.SOCKS5Port = v
			}
		}).
		AddInputField("混合端口", strconv.Itoa(cfg.Mihomo.MixedPort), 10, nil, func(text string) {
			if v, err := strconv.Atoi(text); err == nil {
				cfg.Mihomo.MixedPort = v
			}
		}).
		AddInputField("Redir 端口", strconv.Itoa(cfg.Mihomo.RedirPort), 10, nil, func(text string) {
			if v, err := strconv.Atoi(text); err == nil {
				cfg.Mihomo.RedirPort = v
			}
		}).
		AddInputField("TProxy 端口", strconv.Itoa(cfg.Mihomo.TProxyPort), 10, nil, func(text string) {
			if v, err := strconv.Atoi(text); err == nil {
				cfg.Mihomo.TProxyPort = v
			}
		}).
		AddCheckbox("允许局域网", cfg.Mihomo.AllowLan, func(checked bool) {
			cfg.Mihomo.AllowLan = checked
			doSave("允许局域网")
		}).
		AddCheckbox("IPv6", cfg.Mihomo.IPv6, func(checked bool) {
			cfg.Mihomo.IPv6 = checked
			doSave("IPv6")
		}).
		AddCheckbox("统一延迟", cfg.Mihomo.UnifiedDelay, func(checked bool) {
			cfg.Mihomo.UnifiedDelay = checked
			doSave("统一延迟")
		}).
		AddCheckbox("自动透明代理", cfg.Mihomo.AutoRedirect, func(checked bool) {
			cfg.Mihomo.AutoRedirect = checked
			doSave("自动透明代理")
		}).
		AddDropDown("日志级别", []string{"DEBUG", "INFO", "WARNING", "ERROR", "SILENT"}, logIdx, func(option string, optionIndex int) {
			cfg.Mihomo.LogLevel = logLevels[optionIndex]
			doSave("日志级别")
		}).
		AddInputField("延迟测试链接", cfg.Mihomo.TestURL, 50, nil, func(text string) {
			cfg.Mihomo.TestURL = text
		})

	// 为 mihomoForm 的每个 InputField 设置 blur 保存
	for i := 0; i < mihomoForm.GetFormItemCount(); i++ {
		item := mihomoForm.GetFormItem(i)
		if inputField, ok := item.(*tview.InputField); ok {
			fieldLabel := inputField.GetLabel()
			inputField.SetFinishedFunc(func(key tcell.Key) {
				doSave(fieldLabel)
			})
		}
	}
	mihomoForm.SetBorder(true).SetTitle(" mihomo 设置 ")

	// ---------- mihomo 版本卡片 ----------
	currentVersion := ""
	latestVersion := ""
	hasNewVersion := false
	isChecking := false

	versionInfo := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	updateStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	updateBtn := tview.NewButton("")
	updateBtn.SetBorder(false)

	refreshVersionCard := func() {
		var infoText string
		if currentVersion == "" {
			infoText = "\n[blue::b]mihomo[-:-:-]\n\n['+mihomotui.ColorMuted+']未安装[-]\n\n['+mihomotui.ColorMuted+']Meta Kernel[-]"
		} else {
			infoText = fmt.Sprintf(
				"\n[blue::b]mihomo[-:-:-]\n\n"+
					"[::b]v%s[-:-:-]\n\n"+
					"['+mihomotui.ColorMuted+']Meta Kernel[-]",
				currentVersion,
			)
		}
		versionInfo.SetText(infoText)

		if isChecking {
			updateStatus.SetText(" ['+mihomotui.ColorWarn+']●[-] 检查中... ")
			updateBtn.SetLabel(" 检查中 ")
		} else if currentVersion == "" {
			// 未安装：始终显示下载安装
			if latestVersion != "" {
				updateStatus.SetText(fmt.Sprintf(" ['+mihomotui.ColorWarn+']●[-] 最新: v%s ", latestVersion))
			} else {
				updateStatus.SetText(" ['+mihomotui.ColorMuted+']●[-] 未安装 ")
			}
			updateBtn.SetLabel(" 下载安装 ")
		} else if hasNewVersion {
			updateStatus.SetText(fmt.Sprintf(" ['+mihomotui.ColorWarn+']●[-] 最新: v%s ", latestVersion))
			updateBtn.SetLabel(fmt.Sprintf(" 更新到 v%s ", latestVersion))
		} else if latestVersion != "" {
			updateStatus.SetText(" ['+mihomotui.ColorOK+']●[-] 已是最新版本 ")
			updateBtn.SetLabel(" 检查更新 ")
		} else {
			updateStatus.SetText(" ['+mihomotui.ColorMuted+']●[-] 点击检查 ")
			updateBtn.SetLabel(" 检查更新 ")
		}
	}

	// 启动时自动检测本地版本（通过 IPC）
	go func() {
		client, err := mihomotui.GetIPCClient()
		var cv string
		if err == nil {
			cv, _ = client.IPCGetMihomoVersion()
		}
		app.QueueUpdateDraw(func() {
			currentVersion = cv
			refreshVersionCard()
		})
	}()

	// 检查最新版本（异步，通过 IPC）
	checkVersion := func() {
		if isChecking {
			return
		}
		isChecking = true
		refreshVersionCard()

		go func() {
			client, err := mihomotui.GetIPCClient()
			var lv string
			if err == nil {
				lv, err = client.IPCGetMihomoLatestVersion()
			}

			app.QueueUpdateDraw(func() {
				isChecking = false
				latestVersion = lv

				if err != nil && lv == "" {
					hasNewVersion = false
					refreshVersionCard()
					showModal("检测失败", "无法获取最新版本信息，请检查网络连接。")
					return
				}

				if currentVersion == "" {
					// 未安装
					hasNewVersion = false
					refreshVersionCard()
					return
				}

				if currentVersion == lv {
					// 已是最新
					hasNewVersion = false
					refreshVersionCard()
					showModal("已是最新版本", fmt.Sprintf("当前版本: v%s", currentVersion))
					return
				}

				// 有新版本
				hasNewVersion = true
				refreshVersionCard()
			})
		}()
	}

	// 执行更新（异步，通过 IPC，带进度弹窗）
	doUpdate := func() {
		if isChecking || latestVersion == "" {
			return
		}
		isChecking = true
		refreshVersionCard()

		// 进度弹窗
		progressText := tview.NewTextView().
			SetTextAlign(tview.AlignCenter).
			SetText("准备下载...")
		progressFlex := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(tview.NewTextView().SetText("正在下载 mihomo 内核").SetTextAlign(tview.AlignCenter), 1, 0, false).
			AddItem(progressText, 1, 0, false)
		progressFlex.SetBorder(true).SetTitle(" 升级 ").SetTitleAlign(tview.AlignCenter)
		progressModalFrame := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(progressFlex, 4, 0, true).
				AddItem(nil, 0, 1, false), 50, 0, true).
			AddItem(nil, 0, 1, false)
		settingsPages.AddPage("upgrade-progress", progressModalFrame, true, true)

		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					settingsPages.HidePage("upgrade-progress")
					settingsPages.RemovePage("upgrade-progress")
					isChecking = false
					updateStatus.SetText(" ['+mihomotui.ColorError+']●[-] 更新失败 ")
					showModal("更新失败", fmt.Sprintf("获取 IPC 客户端失败: %v", err))
					refreshVersionCard()
				})
				return
			}
			// 发起升级请求（服务端异步执行）
			if err := client.IPCUpgradeMihomo(latestVersion); err != nil {
				app.QueueUpdateDraw(func() {
					settingsPages.HidePage("upgrade-progress")
					settingsPages.RemovePage("upgrade-progress")
					isChecking = false
					updateStatus.SetText(" ['+mihomotui.ColorError+']●[-] 更新失败 ")
					showModal("更新失败", fmt.Sprintf("启动升级失败: %v", err))
					refreshVersionCard()
				})
				return
			}

			// 轮询进度
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				progress, err := client.IPCGetMihomoUpgradeProgress()
				if err != nil {
					continue
				}
				app.QueueUpdateDraw(func() {
					switch progress.Status {
					case "downloading":
						bar := buildProgressBar(progress.Percent)
						progressText.SetText(fmt.Sprintf("%s %d%%", bar, progress.Percent))
					case "extracting":
						progressText.SetText("正在解压...")
					case "done":
						settingsPages.HidePage("upgrade-progress")
						settingsPages.RemovePage("upgrade-progress")
						isChecking = false
						currentVersion = latestVersion
						hasNewVersion = false
						updateStatus.SetText(" ['+mihomotui.ColorOK+']●[-] 更新成功 ")
						showModal("更新成功", fmt.Sprintf("mihomo 已更新至 v%s", latestVersion))
						refreshVersionCard()
					case "error":
						settingsPages.HidePage("upgrade-progress")
						settingsPages.RemovePage("upgrade-progress")
						isChecking = false
						updateStatus.SetText(" ['+mihomotui.ColorError+']●[-] 更新失败 ")
						showModal("更新失败", fmt.Sprintf("下载或安装失败: %s", progress.Message))
						refreshVersionCard()
					}
				})
				if progress.Status == "done" || progress.Status == "error" {
					break
				}
			}
		}()
	}

	updateBtn.SetSelectedFunc(func() {
		if currentVersion == "" {
			if latestVersion == "" {
				checkVersion()
			} else {
				doUpdate()
			}
		} else if hasNewVersion {
			doUpdate()
		} else {
			checkVersion()
		}
	})

	refreshVersionCard()

	versionBottom := tview.NewFlex().
		AddItem(updateStatus, 0, 1, false).
		AddItem(updateBtn, 18, 0, true)

	mihomoVersionCard := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(versionInfo, 0, 1, false).
		AddItem(versionBottom, 1, 0, true)
	mihomoVersionCard.SetBorder(true).SetTitle(" 版本信息 ")

	// mihomo 设置内容：左侧表单 + 右侧版本卡片
	mihomoContent := tview.NewFlex().
		AddItem(mihomoForm, 0, 3, true).
		AddItem(mihomoVersionCard, 24, 1, false)

	// ---------- 关于页面 ----------
	aboutText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf(
			"\n\n" +
				"[::b]mihomo-tui[-:-:-]\n\n" +
				"版本: v0.1.0\n" +
				"Go 版本: 1.26.1\n\n" +
				"为 ['+mihomotui.ColorInfo+']mihomo[-] 内核开发的终端 UI 配置工具\n\n" +
				"仓库: github.com/mihomo-tui/mihomo-tui\n\n" +
				"['+mihomotui.ColorMuted+']© 2025 mihomo-tui Team[-]",
		))
	aboutText.SetBorder(true).SetTitle(" 关于 ")

	shutdownBtn := tview.NewButton(" 关闭后台服务并退出 ")
	shutdownBtn.SetBorder(false)
	shutdownBtn.SetSelectedFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				_ = client.IPCShutdownDaemon()
			}
			app.Stop()
		}()
	})

	aboutPage := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(aboutText, 0, 1, false).
		AddItem(shutdownBtn, 1, 0, true)

	// 更新 Tab 样式
	updateTabs := func() {
		if activeTab == 0 {
			tabSystem.SetLabel("[black:blue:b] 系统设置 [-:-:-]")
		} else {
			tabSystem.SetLabel(" 系统设置 ")
		}
		if activeTab == 1 {
			tabMihomo.SetLabel("[black:blue:b] mihomo 设置 [-:-:-]")
		} else {
			tabMihomo.SetLabel(" mihomo 设置 ")
		}
		if activeTab == 2 {
			tabAbout.SetLabel("[black:blue:b] 关于 [-:-:-]")
		} else {
			tabAbout.SetLabel(" 关于 ")
		}
	}

	// 切换内容
	switchContent := func(tab int) {
		activeTab = tab
		content.Clear()
		switch tab {
		case 0:
			content.AddItem(systemForm, 0, 1, true)
		case 1:
			content.AddItem(mihomoContent, 0, 1, true)
		case 2:
			content.AddItem(aboutPage, 0, 1, true)
		}
		updateTabs()
	}

	// Tab 点击事件
	tabSystem.SetSelectedFunc(func() { switchContent(0) })
	tabMihomo.SetSelectedFunc(func() { switchContent(1) })
	tabAbout.SetSelectedFunc(func() { switchContent(2) })

	// 初始化
	updateTabs()
	content.AddItem(systemForm, 0, 1, true)

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabs, 1, 0, true).
		AddItem(content, 0, 1, false)

	settingsPages.AddPage("main", page, true, true)

	return settingsPages
}
