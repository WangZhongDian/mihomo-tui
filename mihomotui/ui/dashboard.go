package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// buildSubCardText 从 globalConfig 构建订阅卡片文本
func buildSubCardText() string {
	cfg := mihomotui.GlobalConfig()
	if len(cfg.Subscriptions) == 0 {
		return "[" + mihomotui.ColorMuted + "] 暂无订阅\n\n 请前往订阅页面导入[-]"
	}
	var sub mihomotui.SubscriptionMeta
	if cfg.ActiveSubscription >= 0 && cfg.ActiveSubscription < len(cfg.Subscriptions) {
		sub = cfg.Subscriptions[cfg.ActiveSubscription]
	} else {
		sub = cfg.Subscriptions[0]
	}
	percent := 0.0
	if sub.TotalGB > 0 {
		percent = sub.UsedGB / sub.TotalGB * 100
	}
	bar := ProgressBar(mihomotui.ProgressBarWidth, percent)
	return fmt.Sprintf(
		"[%s]  %s[-:-:-]  [订阅]\n\n"+
			" 来源: %s\n"+
			" 更新: %s\n"+
			" 流量: %.2fGB / %.2fGB\n\n"+
			" %.0f%%\n"+
			" %s────────────────────",
		mihomotui.ColorHeader, sub.Name, mihomotui.RedactURL(sub.URL), sub.UpdatedAt,
		sub.UsedGB, sub.TotalGB, percent, bar,
	)
}

// NewDashboard 创建首页仪表盘页面
func NewDashboard(app *tview.Application) (Page, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	// 订阅信息卡片
	subCard := tview.NewTextView().
		SetText(buildSubCardText()).
		SetDynamicColors(true)
	subCard.SetBorder(true).
		SetTitle(" 订阅 ").
		SetTitleAlign(tview.AlignLeft)

	// 当前节点卡片（动态更新）
	nodeCard := tview.NewTextView().
		SetText(" [" + mihomotui.ColorMuted + "]○[-:-:-] 内核未运行\n    暂无节点信息").
		SetDynamicColors(true)
	nodeCard.SetBorder(true).
		SetTitle(" 当前节点 ").
		SetTitleAlign(tview.AlignLeft)

	updateNodeCard := func() {
		go func() {
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				return
			}
			groups, err := api.GetProxyGroups()
			if err != nil {
				return
			}

			cfg := mihomotui.GlobalConfig()
			proxyMode := cfg.ProxyMode

			// 构建 group 查找映射
			groupMap := make(map[string]*mihomotui.ProxyGroup, len(groups))
			for i := range groups {
				groupMap[groups[i].Name] = &groups[i]
			}

			var text string

			switch proxyMode {
			case "direct":
				text = fmt.Sprintf(
					" [%s::b]●[-:-:-] 直连模式\n"+
						"    所有流量直接连接，不经过代理",
					mihomotui.ColorOK,
				)
			case "global":
				group := groupMap["GLOBAL"]
				if group != nil && len(group.Nodes) > 0 {
					var activeNode *mihomotui.ProxyNode
					if group.Now != "" {
						for i := range group.Nodes {
							if group.Nodes[i].Name == group.Now {
								activeNode = &group.Nodes[i]
								break
							}
						}
					}
					if activeNode == nil {
						activeNode = &group.Nodes[0]
					}
					delayColor := DelayColor(activeNode.Delay)
					delayText := DelayText(activeNode.Delay)
					text = fmt.Sprintf(
						" [%s::b]●[-:-:-] %s\n"+
							"    %s\n\n"+
							" 代理组: [::u]%s[-:-:-]\n\n"+
							" 节点: [%s]●[-] %s [%s]%s[-]",
						mihomotui.ColorOK, activeNode.Name, activeNode.Type, group.Name,
						mihomotui.ColorOK, activeNode.Name, delayColor, delayText,
					)
				} else {
					text = fmt.Sprintf(
						" [%s::b]●[-:-:-] 全局模式\n"+
							"    暂无可用节点",
						mihomotui.ColorOK,
					)
				}
			default: // rule 及其他模式
				groupName := cfg.DefaultProxyGroup
				group := groupMap[groupName]
				if group == nil && len(groups) > 0 {
					// fallback：优先找 Selector 或 URLTest
					for i := range groups {
						if groups[i].Type == "Selector" || groups[i].Type == "URLTest" {
							group = &groups[i]
							break
						}
					}
				}
				if group == nil {
					return
				}

				// Fallback / LoadBalance 不显示当前节点
				if group.Type == "Fallback" || group.Type == "LoadBalance" {
					text = fmt.Sprintf(
						" [%s::b]●[-:-:-] %s\n"+
							"    %s\n\n"+
							" 代理组: [::u]%s[-:-:-]\n\n"+
							" 模式: 自动切换（%s）",
						mihomotui.ColorOK, group.Name, group.Type, group.Name,
						group.Type,
					)
				} else {
					var activeNode *mihomotui.ProxyNode
					if group.Now != "" {
						for i := range group.Nodes {
							if group.Nodes[i].Name == group.Now {
								activeNode = &group.Nodes[i]
								break
							}
						}
					}
					if activeNode == nil && len(group.Nodes) > 0 {
						activeNode = &group.Nodes[0]
					}
					if activeNode == nil {
						return
					}

					delayColor := DelayColor(activeNode.Delay)
					delayText := DelayText(activeNode.Delay)

					text = fmt.Sprintf(
						" [%s::b]●[-:-:-] %s\n"+
							"    %s\n\n"+
							" 代理组: [::u]%s[-:-:-]\n\n"+
							" 节点: [%s]●[-] %s [%s]%s[-]",
						mihomotui.ColorOK, activeNode.Name, activeNode.Type, group.Name,
						mihomotui.ColorOK, activeNode.Name, delayColor, delayText,
					)
				}
			}

			app.QueueUpdateDraw(func() {
				nodeCard.SetText(text)
			})
		}()
	}

	// 网络设置卡片（可交互）
	netCard, refreshNetCard := newNetSettingsCard(app)

	// 代理模式卡片（可交互）
	modeCard, refreshModeCard := newProxyModeCard()

	// 流量统计卡片
	statsCard := tview.NewTextView().
		SetText(
			" 上传: [" + mihomotui.ColorWarn + "]0.00[-] B/s\n" +
				" 下载: [" + mihomotui.ColorWarn + "]0.00[-] B/s\n" +
				" 总计: [" + mihomotui.ColorWarn + "]0.0[-] MB",
		).
		SetDynamicColors(true)
	statsCard.SetBorder(true).
		SetTitle(" 流量统计 ").
		SetTitleAlign(tview.AlignLeft)

	// 根级 Pages 容器，用于覆盖全屏弹窗
	rootPages := tview.NewPages()
	rootPages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	showModal := func(title, message string) {
		lastFocus := app.GetFocus()
		modal := tview.NewModal().
			SetText(fmt.Sprintf("%s\n\n%s", title, message)).
			AddButtons([]string{"确认"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				rootPages.HidePage("modal")
				rootPages.RemovePage("modal")
				if lastFocus != nil {
					app.SetFocus(lastFocus)
				}
			})
		rootPages.AddPage("modal", modal, true, true)
		app.SetFocus(modal)
	}

	// 内核状态卡片（使用根级 showModal）
	kernelCard := newKernelStatusCard(app, showModal, ctx)

	// 使用 Grid 构建卡片网格布局
	grid := tview.NewGrid().
		SetRows(0, 0, 0).
		SetColumns(0, 0).
		SetBorders(false).
		AddItem(subCard, 0, 0, 1, 1, 0, 0, false).
		AddItem(nodeCard, 0, 1, 1, 1, 0, 0, false).
		AddItem(netCard, 1, 0, 1, 1, 0, 0, true).
		AddItem(modeCard, 1, 1, 1, 1, 0, 0, true).
		AddItem(statsCard, 2, 0, 1, 1, 0, 0, false).
		AddItem(kernelCard, 2, 1, 1, 1, 0, 0, true)
	grid.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	rootPages.AddPage("main", grid, true, true)

	// 流量流 goroutine
	go func() {
		time.Sleep(1 * time.Second)
		updateNodeCard()
		api, err := mihomotui.GetMihomoAPI()
		if err != nil {
			return
		}
		resp, err := api.GetTrafficStream()
		if err != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		totalUp, totalDown := 0.0, 0.0
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				line = strings.TrimSpace(after)
			}
			if line == "" {
				continue
			}
			var traffic struct {
				Up   int64 `json:"up"`
				Down int64 `json:"down"`
			}
			if err := json.Unmarshal([]byte(line), &traffic); err != nil {
				continue
			}
			upSpeed := float64(traffic.Up) / 8
			downSpeed := float64(traffic.Down) / 8
			totalUp += upSpeed
			totalDown += downSpeed
			app.QueueUpdateDraw(func() {
				statsCard.SetText(fmt.Sprintf(
					" 上传: ["+mihomotui.ColorWarn+"]%.2f[-] KB/s\n"+
						" 下载: ["+mihomotui.ColorWarn+"]%.2f[-] KB/s\n"+
						" 总计: ["+mihomotui.ColorWarn+"]%.2f[-] MB",
					upSpeed, downSpeed, (totalUp+totalDown)/1024,
				))
			})
		}
	}()

	// 定时刷新节点卡片
	go func() {
		ticker := time.NewTicker(mihomotui.DefaultRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateNodeCard()
			}
		}
	}()

	refresh := func() {
		refreshNetCard()
		refreshModeCard()
		updateNodeCard()
	}

	page := &pageWrapper{Primitive: rootPages, ctx: ctx, cancel: cancel}
	return page, refresh
}

// newKernelStatusCard 创建内核状态卡片（显示运行状态 + 外部资源 + 启停按钮）
func newKernelStatusCard(app *tview.Application, showModal func(string, string), ctx context.Context) tview.Primitive {
	statusText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	resText := tview.NewTextView().
		SetDynamicColors(true)

	actionBtn := tview.NewButton(" 启动内核 ")
	actionBtn.SetBorder(false)
	resBtn := tview.NewButton(" 下载外部资源 ")
	resBtn.SetBorder(false)

	isRunning := false
	pid := 0
	installedVersion := ""
	resourceInfos := []mihomotui.ExternalResourceInfo{}
	launchMode := ""

	updateResText := func() {
		var sb strings.Builder
		sb.WriteString("[::b] 外部资源[-:-:-]\n")
		allExist := true
		for _, r := range resourceInfos {
			if r.Exists {
				fmt.Fprintf(&sb, " ["+mihomotui.ColorOK+"]●[-] %s  %s\n",
					r.Name, mihomotui.FormatSize(r.Size))
			} else {
				fmt.Fprintf(&sb, " ["+mihomotui.ColorMuted+"]○[-] %s  未下载\n", r.Name)
				allExist = false
			}
		}
		resText.SetText(sb.String())
		if allExist {
			resBtn.SetLabel(" 更新外部资源 ")
		} else {
			resBtn.SetLabel(" 下载外部资源 ")
		}
	}

	modeText := func() string {
		if launchMode == "embedded" {
			return "[" + mihomotui.ColorInfo + "]一体服务[-]"
		}
		return "[" + mihomotui.ColorWarn + "]分体服务[-]"
	}

	updateStatus := func() {
		var binaryStatus string
		if installedVersion != "" {
			binaryStatus = fmt.Sprintf("["+mihomotui.ColorOK+"]●[-] 内核已安装 v%s", installedVersion)
		} else {
			binaryStatus = "[" + mihomotui.ColorMuted + "]○[-] 内核未安装"
		}
		if isRunning {
			statusText.SetText(fmt.Sprintf(
				"["+mihomotui.ColorOK+"::b]●[-:-:-] 运行中  %s\n PID: %d\n%s",
				modeText(), pid, binaryStatus,
			))
			actionBtn.SetLabel(" 停止内核 ")
		} else {
			statusText.SetText(fmt.Sprintf(
				"["+mihomotui.ColorMuted+"]○[-:-:-] 已停止  %s\n 内核未运行\n%s",
				modeText(), binaryStatus,
			))
			actionBtn.SetLabel(" 启动内核 ")
		}
		updateResText()
	}

	refreshStatus := func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				return
			}
			status, err := client.IPCGetMihomoStatus()
			if err != nil {
				return
			}
			version, _ := client.IPCGetMihomoVersion()
			resources, _ := client.IPCCheckExternalResources()
			info, _ := client.IPCGetDaemonInfo()
			app.QueueUpdateDraw(func() {
				isRunning = status.Running
				pid = status.PID
				installedVersion = version
				if resources != nil {
					resourceInfos = resources
				}
				if info != nil {
					launchMode = info.LaunchMode
				}
				updateStatus()
			})
		}()
	}

	actionBtn.SetSelectedFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("连接失败", err.Error())
				})
				return
			}
			if isRunning {
				if err := client.IPCStopMihomo(); err != nil {
					app.QueueUpdateDraw(func() {
						showModal("停止失败", err.Error())
					})
					return
				}
			} else {
				if err := client.IPCStartMihomo(); err != nil {
					app.QueueUpdateDraw(func() {
						showModal("启动失败", err.Error())
					})
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
			refreshStatus()
		}()
	})

	resBtn.SetSelectedFunc(func() {
		go func() {
			app.QueueUpdateDraw(func() {
				resBtn.SetLabel(" 下载中... ")
			})
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				err = client.IPCDownloadExternalResources()
			}
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("下载失败", err.Error())
					updateResText()
				})
				return
			}
			app.QueueUpdateDraw(func() {
				showModal("下载成功", "外部资源已更新")
				updateResText()
			})
		}()
	})

	updateStatus()

	buttons := tview.NewFlex().
		AddItem(actionBtn, 0, 1, true).
		AddItem(resBtn, 0, 1, true)

	card := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(statusText, 0, 1, false).
		AddItem(resText, 0, 1, false).
		AddItem(buttons, 1, 0, true)
	card.SetBorder(true).SetTitle(" 内核 ")
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	// 定时刷新状态
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
			refreshStatus()
		}
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshStatus()
			}
		}
	}()

	return card
}

// newNetSettingsCard 创建可交互的网络设置卡片（状态实时同步）
func newNetSettingsCard(app *tview.Application) (tview.Primitive, func()) {
	activeTab := 0
	daemonIsRoot := false

	content := tview.NewTextView().SetDynamicColors(true)

	tabProxy := tview.NewButton(" ■ 系统代理 ")
	tabProxy.SetBorder(false)
	tabTun := tview.NewButton(" □ 虚拟网卡模式 ")
	tabTun.SetBorder(false)

	statusIndicator := tview.NewTextView().SetDynamicColors(true)
	toggleBtn := tview.NewButton(" 关闭 ")
	toggleBtn.SetBorder(false)

	update := func() {
		cfg := mihomotui.GlobalConfig()
		proxyEnabled := cfg.System.SystemProxy
		tunEnabled := cfg.System.TUN
		if activeTab == 0 {
			tabProxy.SetLabel(" ■ 系统代理 ")
			tabTun.SetLabel(" □ 虚拟网卡模式 ")
			if proxyEnabled {
				content.SetText("[black:green] 已开启 [-:-:-]\n\n 系统代理已启用，您的应用将通过代理访问网络")
				statusIndicator.SetText(" [" + mihomotui.ColorOK + "::b]●[-:-:-] 运行中 ")
				toggleBtn.SetLabel(" 关闭 ")
			} else {
				content.SetText("[black:gray] 已关闭 [-:-:-]\n\n 系统代理已关闭")
				statusIndicator.SetText(" [" + mihomotui.ColorMuted + "]○[-:-:-] 已停止 ")
				toggleBtn.SetLabel(" 开启 ")
			}
		} else {
			tabProxy.SetLabel(" □ 系统代理 ")
			tabTun.SetLabel(" ■ 虚拟网卡模式 ")
			if tunEnabled {
				content.SetText("[black:green] 已开启 [-:-:-]\n\n 虚拟网卡模式已启用")
				statusIndicator.SetText(" [" + mihomotui.ColorOK + "::b]●[-:-:-] 运行中 ")
				toggleBtn.SetLabel(" 关闭 ")
			} else {
				var msg string
				if !daemonIsRoot {
					msg = "[black:gray] 已关闭 [-:-:-]\n\n 虚拟网卡模式已关闭\n\n [" + mihomotui.ColorError + "]⚠ 服务端未以 root 权限运行，无法使用 TUN 模式[-]"
				} else {
					msg = "[black:gray] 已关闭 [-:-:-]\n\n 虚拟网卡模式已关闭"
				}
				content.SetText(msg)
				statusIndicator.SetText(" [" + mihomotui.ColorMuted + "]○[-:-:-] 已停止 ")
				toggleBtn.SetLabel(" 开启 ")
			}
		}
	}

	// 异步获取守护进程权限信息
	go func() {
		client, err := mihomotui.GetIPCClient()
		if err != nil {
			return
		}
		info, err := client.IPCGetDaemonInfo()
		if err != nil {
			return
		}
		daemonIsRoot = info.IsRoot
		if app != nil {
			app.QueueUpdateDraw(update)
		}
	}()

	tabProxy.SetSelectedFunc(func() {
		activeTab = 0
		update()
	})

	tabTun.SetSelectedFunc(func() {
		activeTab = 1
		update()
	})

	toggleBtn.SetSelectedFunc(func() {
		cfg := mihomotui.GlobalConfig()
		if activeTab == 0 {
			cfg.System.SystemProxy = !cfg.System.SystemProxy
			if err := cfg.SetSystemProxyEnv(cfg.System.SystemProxy); err != nil {
				mihomotui.Warnf("系统代理环境变量设置失败: %v", err)
			}
		} else {
			cfg.System.TUN = !cfg.System.TUN
		}
		mihomotui.SetGlobalConfig(*cfg)
		update()
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				mihomotui.Warnf("网络设置同步失败: %v", err)
				return
			}
			if err := client.IPCUpdateConfig(cfg); err != nil {
				mihomotui.Warnf("网络设置同步失败: %v", err)
			}
		}()
	})

	update()

	tabs := tview.NewFlex().
		AddItem(tabProxy, 0, 1, true).
		AddItem(tabTun, 0, 1, true)

	bottom := tview.NewFlex().
		AddItem(statusIndicator, 0, 1, false).
		AddItem(toggleBtn, 10, 0, true)

	card := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabs, 1, 0, true).
		AddItem(content, 0, 1, false).
		AddItem(bottom, 1, 0, true)

	card.SetBorder(true).SetTitle(" 网络设置 ")
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	return card, update
}

// newProxyModeCard 创建可交互的代理模式卡片
func newProxyModeCard() (tview.Primitive, func()) {
	modes := []string{"规则", "全局", "直连"}
	modeValues := []string{"rule", "global", "direct"}
	descriptions := []string{
		"\n 基于预设规则智能判断流量走向",
		"\n 所有流量通过代理节点转发",
		"\n 所有流量直接连接，不经过代理",
	}

	cfg := mihomotui.GlobalConfig()
	activeMode := 0
	for i, v := range modeValues {
		if cfg.ProxyMode == v {
			activeMode = i
			break
		}
	}

	content := tview.NewTextView().SetDynamicColors(true)

	tabViews := make([]*tview.TextView, len(modes))
	tabs := tview.NewFlex()

	update := func() {
		for i, tv := range tabViews {
			if i == activeMode {
				tv.SetText(fmt.Sprintf("[white:blue:b] ▶ %s [-:-:-]", modes[i]))
			} else {
				tv.SetText(fmt.Sprintf("["+mihomotui.ColorMuted+"]   %s [-]", modes[i]))
			}
		}
		content.SetText(descriptions[activeMode])
	}

	for i := range modes {
		idx := i
		tv := tview.NewTextView().
			SetDynamicColors(true).
			SetTextAlign(tview.AlignCenter)
		tv.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftClick {
				activeMode = idx
				update()
				go func(mode string) {
					cfg := mihomotui.GlobalConfig()
					cfg.ProxyMode = mode
					mihomotui.SetGlobalConfig(*cfg)
					client, err := mihomotui.GetIPCClient()
					if err != nil {
						mihomotui.Warnf("切换代理模式失败: 无法获取 IPC 客户端: %v", err)
						return
					}
					if err := client.IPCUpdateConfig(cfg); err != nil {
						mihomotui.Warnf("切换代理模式失败: %v", err)
					}
				}(modeValues[idx])
			}
			return action, event
		})
		tabViews[i] = tv
		tabs.AddItem(tv, 0, 1, true)
	}

	update()

	card := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabs, 1, 0, true).
		AddItem(content, 0, 1, false)
	card.SetBorder(true).SetTitle(" 代理模式 ")
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	refresh := func() {
		cfg := mihomotui.GlobalConfig()
		for i, v := range modeValues {
			if cfg.ProxyMode == v {
				activeMode = i
				break
			}
		}
		update()
	}

	return card, refresh
}
