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

// buildSubCardText displays the pools currently exposed to the running mihomo configuration.
func buildSubCardText() string {
	cfg := mihomotui.GlobalConfig()
	if len(cfg.SubscriptionPools) == 0 {
		return "[" + mihomotui.ColorMuted + "]" + T("dashboard.pool.none_running") + "[-]"
	}
	byID := make(map[string]mihomotui.SubscriptionMeta, len(cfg.Subscriptions))
	for _, sub := range cfg.Subscriptions {
		byID[sub.ID] = sub
	}
	var lines []string
	// 最近一次运行时应用失败时，内核仍在跑上一次成功生成的旧配置，
	// 首页必须先给出这条结论，避免用户把旧节点误认为当前配置。
	if status := mihomotui.LastRuntimeApply(); status.HasApplied && !status.Applied {
		reason := strings.TrimSpace(status.Error)
		if reason == "" {
			reason = T("dashboard.unknown_reason")
		}
		lines = append(lines, Tf("dashboard.apply_failed", map[string]any{"Stage": status.Stage, "Reason": reason}))
		lines = append(lines, T("dashboard.apply_failed_hint"), "")
	}
	for _, pool := range cfg.SubscriptionPools {
		mode := T("dashboard.pool.mode.failover")
		if pool.Mode == mihomotui.SubscriptionPoolModeMerge {
			mode = T("dashboard.pool.mode.merge")
		}
		state := "[green]●[-]"
		if pool.Degraded {
			state = "[yellow]▲[-]"
		}
		if !pool.Enabled {
			// 禁用的池同样列出：否则用户只会看到"暂无已启用订阅池"，
			// 却不知道成员仍在池里但没有参与运行。
			state = "[red]■[-]"
		}
		lines = append(lines, fmt.Sprintf("%s [::b]%s[-]  (%s)", state, SanitizeDisplayText(pool.Name), mode))
		availability := cfg.PoolAvailability(pool)
		if pool.Mode == mihomotui.SubscriptionPoolModeMerge {
			lines = append(lines, Tf("dashboard.pool.merge_availability", map[string]any{"Available": availability.Available, "Total": availability.Total}))
		} else if _, ok := byID[pool.ActiveMemberID]; !ok {
			lines = append(lines, T("dashboard.pool.active_missing"))
		}
		if !pool.Enabled {
			lines = append(lines, T("dashboard.pool.disabled_hint"))
		}
		// 在池摘要下逐行展示成员，方便从首页直接确认参与运行的订阅源健康和额度。
		for _, id := range pool.Members {
			sub, ok := byID[id]
			if !ok {
				continue
			}
			status := T("dashboard.subscription.healthy")
			if !mihomotui.SubscriptionCacheAvailable(sub) {
				status = T("dashboard.subscription.unavailable")
				if sub.FailureCount > 0 {
					status = Tf("dashboard.subscription.failure_count", map[string]any{"Count": sub.FailureCount})
				}
			}
			current := ""
			if pool.Mode != mihomotui.SubscriptionPoolModeMerge && id == pool.ActiveMemberID {
				current = T("dashboard.subscription.current")
			}
			line := fmt.Sprintf("  • %s%s · %s", SanitizeDisplayText(sub.Name), current, status)
			if remaining := subscriptionRemainingText(sub); remaining != "" {
				line += " · " + T("dashboard.subscription.remaining") + " " + remaining
			}
			lines = append(lines, line)
		}
		if pool.Degraded && pool.LastSwitchReason != "" {
			lines = append(lines, "  "+T("dashboard.pool.degraded")+": "+pool.LastSwitchReason)
		}
	}
	if len(lines) == 0 {
		return "[" + mihomotui.ColorMuted + "]" + T("dashboard.pool.none_enabled") + "[-]"
	}
	return strings.Join(lines, "\n")
}

func subscriptionRemainingText(sub mihomotui.SubscriptionMeta) string {
	remaining := sub.RemainingBytes
	if remaining <= 0 && sub.TotalBytes > 0 {
		remaining = sub.TotalBytes - sub.UploadBytes - sub.DownloadBytes
	}
	if remaining <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fGB", float64(remaining)/(1024*1024*1024))
}

func subscriptionQuotaText(sub mihomotui.SubscriptionMeta) string {
	if sub.RemainingBytes > 0 && sub.TotalBytes == 0 {
		return T("dashboard.subscription.remaining") + fmt.Sprintf(" %.2fGB", float64(sub.RemainingBytes)/(1024*1024*1024))
	}
	if sub.TotalGB > 0 {
		return fmt.Sprintf("%.2fGB / %.2fGB", sub.UsedGB, sub.TotalGB)
	}
	return T("dashboard.subscription.metadata_invalid")
}

// NewDashboard 创建首页仪表盘页面
func NewDashboard(app *tview.Application) (Page, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	// 根级 Pages 容器，用于覆盖全屏弹窗（卡片回调需要它，因此先创建）
	rootPages := tview.NewPages()
	rootPages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	showModal := func(title, message string) {
		ShowAlertModal(app, rootPages, title, message)
	}

	// 订阅信息卡片
	subCard := tview.NewTextView().
		SetText(buildSubCardText()).
		SetDynamicColors(true)
	subCard.SetBorder(true).
		SetTitle(" " + T("dashboard.pool.title") + " ").
		SetTitleAlign(tview.AlignLeft)

	// 当前节点卡片（动态更新）
	nodeCard := tview.NewTextView().
		SetText(T("dashboard.node.initial")).
		SetDynamicColors(true)
	nodeCard.SetBorder(true).
		SetTitle(" " + T("dashboard.node.title") + " ").
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
				text = T("dashboard.node.direct")
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
					// 节点/策略组名来自订阅，可能含国旗 emoji：按显示宽度归一化，
					// 否则部分终端会把国旗渲染成 4 格并与后面的文字重叠。
					nodeName := SanitizeDisplayText(activeNode.Name)
					nodeType := SanitizeDisplayText(activeNode.Type)
					groupLabel := SanitizeDisplayText(group.Name)
					text = Tf("dashboard.node.active", map[string]any{
						"Color": mihomotui.ColorOK, "Node": nodeName, "Type": nodeType, "Group": groupLabel,
						"DelayColor": delayColor, "Delay": delayText,
					})
				} else {
					text = T("dashboard.node.global_empty")
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
					text = Tf("dashboard.node.automatic", map[string]any{
						"Color": mihomotui.ColorOK,
						"Group": SanitizeDisplayText(group.Name), "Type": SanitizeDisplayText(group.Type),
					})
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

					// 节点/策略组名来自订阅，可能含国旗 emoji：按显示宽度归一化，
					// 否则部分终端会把国旗渲染成 4 格并与后面的文字重叠。
					nodeName := SanitizeDisplayText(activeNode.Name)
					nodeType := SanitizeDisplayText(activeNode.Type)
					groupLabel := SanitizeDisplayText(group.Name)
					text = Tf("dashboard.node.active", map[string]any{
						"Color": mihomotui.ColorOK, "Node": nodeName, "Type": nodeType, "Group": groupLabel,
						"DelayColor": delayColor, "Delay": delayText,
					})
				}
			}

			app.QueueUpdateDraw(func() {
				nodeCard.SetText(text)
			})
		}()
	}

	// 网络设置卡片（可交互）
	netCard, refreshNetCard := newNetSettingsCard(app, rootPages)

	// 代理模式卡片（可交互）
	modeCard, refreshModeCard := newProxyModeCard(app)
	// 流量统计卡片
	statsCard := tview.NewTextView().
		SetText(Tf("dashboard.traffic.initial", map[string]any{"Color": mihomotui.ColorWarn})).
		SetDynamicColors(true)
	statsCard.SetBorder(true).
		SetTitle(" " + T("dashboard.traffic.title") + " ").
		SetTitleAlign(tview.AlignLeft)

	// 根级 Pages 容器与弹窗（已在页面开头创建，供各卡片共用）

	// 内核状态卡片（使用根级 showModal）。停止内核会自动关闭系统代理，
	// 因此把网络设置卡片的刷新回调传进去，保证显示与实际一致。
	kernelCard := newKernelStatusCard(app, showModal, ctx, refreshNetCard)

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
				statsCard.SetText(Tf("dashboard.traffic.live", map[string]any{
					"Color": mihomotui.ColorWarn, "Up": upSpeed, "Down": downSpeed, "Total": (totalUp + totalDown) / 1024,
				}))
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
				// 同步 daemon 的运行时应用结果，使"配置未生效"提示能随状态变化。
				if client, cerr := mihomotui.GetIPCClient(); cerr == nil {
					_, _ = client.IPCGetMihomoStatus()
				}
				app.QueueUpdateDraw(func() { subCard.SetText(buildSubCardText()) })
			}
		}
	}()

	refresh := func() {
		subCard.SetText(buildSubCardText())
		refreshNetCard()
		refreshModeCard()
		updateNodeCard()
	}

	page := &pageWrapper{Primitive: rootPages, ctx: ctx, cancel: cancel}
	return page, refresh
}

// newKernelStatusCard 创建内核状态卡片（显示运行状态 + 外部资源 + 启停按钮）
// onKernelStopped 在停止内核后调用（此时系统代理已被自动关闭），用于刷新相关卡片显示。
func newKernelStatusCard(app *tview.Application, showModal func(string, string), ctx context.Context, onKernelStopped func()) tview.Primitive {
	statusText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	resText := tview.NewTextView().
		SetDynamicColors(true)

	actionBtn := tview.NewButton(" " + T("dashboard.kernel.start") + " ")
	actionBtn.SetBorder(false)
	resBtn := tview.NewButton(" " + T("dashboard.resources.download") + " ")
	resBtn.SetBorder(false)

	isRunning := false
	pid := 0
	installedVersion := ""
	resourceInfos := []mihomotui.ExternalResourceInfo{}
	launchMode := ""

	updateResText := func() {
		var sb strings.Builder
		sb.WriteString("[::b] " + T("dashboard.resources.title") + "[-:-:-]\n")
		allExist := true
		for _, r := range resourceInfos {
			if r.Exists {
				fmt.Fprintf(&sb, " ["+mihomotui.ColorOK+"]●[-] %s  %s\n",
					r.Name, mihomotui.FormatSize(r.Size))
			} else {
				fmt.Fprintf(&sb, " ["+mihomotui.ColorMuted+"]○[-] %s  "+T("dashboard.resources.not_downloaded")+"\n", r.Name)
				allExist = false
			}
		}
		resText.SetText(sb.String())
		if allExist {
			resBtn.SetLabel(" " + T("dashboard.resources.update") + " ")
		} else {
			resBtn.SetLabel(" " + T("dashboard.resources.download") + " ")
		}
	}

	modeText := func() string {
		if launchMode == "embedded" {
			return "[" + mihomotui.ColorInfo + "]" + T("dashboard.kernel.embedded") + "[-]"
		}
		return "[" + mihomotui.ColorWarn + "]" + T("dashboard.kernel.standalone") + "[-]"
	}

	updateStatus := func() {
		var binaryStatus string
		if installedVersion != "" {
			binaryStatus = Tf("dashboard.kernel.installed", map[string]any{"Color": mihomotui.ColorOK, "Version": installedVersion})
		} else {
			binaryStatus = "[" + mihomotui.ColorMuted + "]○[-] " + T("dashboard.kernel.not_installed")
		}
		if isRunning {
			statusText.SetText(Tf("dashboard.kernel.running", map[string]any{
				"Color": mihomotui.ColorOK, "Mode": modeText(), "PID": pid, "Binary": binaryStatus,
			}))
			actionBtn.SetLabel(" " + T("dashboard.kernel.stop") + " ")
		} else {
			statusText.SetText(Tf("dashboard.kernel.stopped", map[string]any{
				"Color": mihomotui.ColorMuted, "Mode": modeText(), "Binary": binaryStatus,
			}))
			actionBtn.SetLabel(" " + T("dashboard.kernel.start") + " ")
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
					showModal(T("dashboard.connection_failed"), err.Error())
				})
				return
			}
			if isRunning {
				if err := client.IPCStopMihomo(); err != nil {
					app.QueueUpdateDraw(func() {
						showModal(T("dashboard.kernel.stop_failed"), err.Error())
					})
					return
				}
				// 内核停掉后代理端口不再监听：同步关闭系统代理，
				// 否则新开终端会拿到指向死端口的变量。
				disabled, derr := mihomotui.DisableSystemProxyOnKernelStop()
				if derr != nil {
					mihomotui.Warnf("停止内核后关闭系统代理失败: %v", derr)
				}
				if onKernelStopped != nil {
					app.QueueUpdateDraw(onKernelStopped)
				}
				if disabled {
					app.QueueUpdateDraw(func() {
						showModal(T("dashboard.kernel.stopped_title"), T("dashboard.kernel.proxy_auto_disabled")+"\n\n"+
							mihomotui.SystemProxyClearHint())
					})
				}
			} else {
				if err := client.IPCStartMihomo(); err != nil {
					app.QueueUpdateDraw(func() {
						showModal(T("dashboard.kernel.start_failed"), err.Error())
					})
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
			refreshStatus()
		}()
	})

	externalManualHelp := func(cause error) string {
		var sb strings.Builder
		sb.WriteString(cause.Error())
		sb.WriteString("\n\n" + T("dashboard.resources.manual_help"))
		for _, resource := range resourceInfos {
			if resource.Path == "" {
				continue
			}
			fmt.Fprintf(&sb, "\n%s: %s", resource.Name, resource.Path)
		}
		return sb.String()
	}

	resBtn.SetSelectedFunc(func() {
		go func() {
			app.QueueUpdateDraw(func() {
				resBtn.SetLabel(" " + T("dashboard.resources.downloading") + " ")
			})
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				err = client.IPCDownloadExternalResources()
			}
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("dashboard.resources.download_failed"), externalManualHelp(err))
					updateResText()
				})
				return
			}
			app.QueueUpdateDraw(func() {
				showModal(T("dashboard.resources.download_succeeded"), T("dashboard.resources.updated"))
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
	card.SetBorder(true).SetTitle(" " + T("dashboard.kernel.title") + " ")
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
// pages 由仪表盘传入，用于在卡片内弹出确认/结果弹窗。
func newNetSettingsCard(app *tview.Application, pages *tview.Pages) (tview.Primitive, func()) {
	activeTab := 0
	daemonIsRoot := false

	content := tview.NewTextView().SetDynamicColors(true)

	tabProxy := tview.NewButton(" ■ " + T("dashboard.network.system_proxy") + " ")
	tabProxy.SetBorder(false)
	tabTun := tview.NewButton(" □ " + T("dashboard.network.tun") + " ")
	tabTun.SetBorder(false)

	statusIndicator := tview.NewTextView().SetDynamicColors(true)
	toggleBtn := tview.NewButton(" " + T("dashboard.network.disable") + " ")
	toggleBtn.SetBorder(false)

	update := func() {
		cfg := mihomotui.GlobalConfig()
		// 系统代理开关是本用户偏好，读取本地客户端偏好而非 daemon 配置
		proxyEnabled := mihomotui.GetSystemProxyPreference()
		tunEnabled := cfg.System.TUN
		if activeTab == 0 {
			tabProxy.SetLabel(" ■ " + T("dashboard.network.system_proxy") + " ")
			tabTun.SetLabel(" □ " + T("dashboard.network.tun") + " ")
			if proxyEnabled {
				content.SetText(T("dashboard.network.proxy_enabled"))
				statusIndicator.SetText(" [" + mihomotui.ColorOK + "::b]●[-:-:-] " + T("dashboard.status.running") + " ")
				toggleBtn.SetLabel(" " + T("dashboard.network.disable") + " ")
			} else {
				content.SetText(T("dashboard.network.proxy_disabled"))
				statusIndicator.SetText(" [" + mihomotui.ColorMuted + "]○[-:-:-] " + T("dashboard.status.stopped") + " ")
				toggleBtn.SetLabel(" " + T("dashboard.network.enable") + " ")
			}
		} else {
			tabProxy.SetLabel(" □ " + T("dashboard.network.system_proxy") + " ")
			tabTun.SetLabel(" ■ " + T("dashboard.network.tun") + " ")
			if tunEnabled {
				content.SetText(T("dashboard.network.tun_enabled"))
				statusIndicator.SetText(" [" + mihomotui.ColorOK + "::b]●[-:-:-] " + T("dashboard.status.running") + " ")
				toggleBtn.SetLabel(" " + T("dashboard.network.disable") + " ")
			} else {
				var msg string
				if !daemonIsRoot {
					msg = T("dashboard.network.tun_disabled_root_required_prefix") + mihomotui.ColorError + T("dashboard.network.tun_disabled_root_required_suffix")
				} else {
					msg = T("dashboard.network.tun_disabled")
				}
				content.SetText(msg)
				statusIndicator.SetText(" [" + mihomotui.ColorMuted + "]○[-:-:-] " + T("dashboard.status.stopped") + " ")
				toggleBtn.SetLabel(" " + T("dashboard.network.enable") + " ")
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
		systemProxyTab := activeTab == 0
		if systemProxyTab {
			// 系统代理是当前 TUI 用户的本地偏好：写本用户环境变量 + 本地偏好文件，
			// 不写入 daemon 全局配置，避免多用户共享 daemon 时互相覆盖。
			enable := !mihomotui.GetSystemProxyPreference()
			toggleSystemProxy(app, pages, enable, update)
			return
		}
		current := mihomotui.GlobalConfig()
		enable := !current.System.TUN
		go func() {
			// 基于服务端最新配置提交单字段变更，避免整份覆盖并发修改
			if _, err := mihomotui.MutateServerConfig(func(cfg *mihomotui.Config) {
				cfg.System.TUN = enable
			}); err != nil {
				mihomotui.Warnf("网络设置同步失败: %v", err)
			}
			if app != nil {
				app.QueueUpdateDraw(update)
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

	card.SetBorder(true).SetTitle(" " + T("dashboard.network.title") + " ")
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	return card, update
}

// newProxyModeCard 创建可交互的代理模式卡片
func newProxyModeCard(app *tview.Application) (tview.Primitive, func()) {
	modes := []string{T("dashboard.mode.rule"), T("dashboard.mode.global"), T("dashboard.mode.direct")}
	modeValues := []string{"rule", "global", "direct"}
	descriptions := []string{
		T("dashboard.mode.rule_description"),
		T("dashboard.mode.global_description"),
		T("dashboard.mode.direct_description"),
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

	// refresh 在下方定义（从本地缓存恢复显示），点击回调失败时用于回滚显示
	var refresh func()

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
				mode := modeValues[idx]
				if mode == mihomotui.GlobalConfig().ProxyMode {
					return action, event // 模式未变化，不触发保存
				}
				activeMode = idx
				update()
				go func() {
					// 基于服务端最新配置提交单字段变更，避免整份覆盖并发修改
					if _, err := mihomotui.MutateServerConfig(func(cfg *mihomotui.Config) {
						cfg.ProxyMode = mode
					}); err != nil {
						mihomotui.Warnf("切换代理模式失败: %v", err)
						// 保存失败：从本地缓存恢复显示为当前生效模式
						if app != nil {
							app.QueueUpdateDraw(refresh)
						}
					}
				}()
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
	card.SetBorder(true).SetTitle(" " + T("dashboard.mode.title") + " ")
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	refresh = func() {
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
