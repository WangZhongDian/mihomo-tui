package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// NewRulesPage 创建规则页面（包含规则列表和规则订阅两个 Tab）
func NewRulesPage(app *tview.Application) tview.Primitive {
	pages := tview.NewPages()
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	activeTab := 0 // 0 = 规则列表, 1 = 规则订阅, 2 = 自定义规则, 3 = 内置规则

	// ===== 规则列表页面 =====
	rulesListPage := newRulesListPage(app, pages)

	// ===== 规则订阅页面 =====
	ruleProviderPage := newRuleProviderPage(app, pages)

	// ===== 自定义规则页面 =====
	customRulesPage := newCustomRulesPage(app, pages)

	// ===== 内置规则页面 =====
	builtInRulesPage := newBuiltInRulesPage(app, pages)

	pages.AddPage("rules_list", rulesListPage, true, true)
	pages.AddPage("rule_providers", ruleProviderPage, true, false)
	pages.AddPage("custom_rules", customRulesPage, true, false)
	pages.AddPage("builtin_rules", builtInRulesPage, true, false)

	// ===== Tab 栏（三个 Button 水平排列）=====
	tab1Btn := tview.NewButton(" 规则列表 ")
	tab1Btn.SetBorder(false)
	tab2Btn := tview.NewButton(" 规则订阅 ")
	tab2Btn.SetBorder(false)
	tab3Btn := tview.NewButton(" 自定义规则 ")
	tab3Btn.SetBorder(false)
	tab4Btn := tview.NewButton(" 内置规则 ")
	tab4Btn.SetBorder(false)

	updateTabHighlight := func() {
		tab1Btn.SetLabel(" 规则列表 ")
		tab2Btn.SetLabel(" 规则订阅 ")
		tab3Btn.SetLabel(" 自定义规则 ")
		tab4Btn.SetLabel(" 内置规则 ")
		switch activeTab {
		case 0:
			tab1Btn.SetLabel("[规则列表]")
		case 1:
			tab2Btn.SetLabel("[规则订阅]")
		case 2:
			tab3Btn.SetLabel("[自定义规则]")
		case 3:
			tab4Btn.SetLabel("[内置规则]")
		}
	}
	updateTabHighlight()

	switchToTab := func(tab int) {
		if tab == activeTab {
			return
		}
		activeTab = tab
		updateTabHighlight()
		switch activeTab {
		case 0:
			pages.SwitchToPage("rules_list")
			app.SetFocus(rulesListPage)
		case 1:
			pages.SwitchToPage("rule_providers")
			app.SetFocus(ruleProviderPage)
		case 2:
			pages.SwitchToPage("custom_rules")
			app.SetFocus(customRulesPage)
		case 3:
			pages.SwitchToPage("builtin_rules")
			app.SetFocus(builtInRulesPage)
		}
	}

	tab1Btn.SetSelectedFunc(func() { switchToTab(0) })
	tab2Btn.SetSelectedFunc(func() { switchToTab(1) })
	tab3Btn.SetSelectedFunc(func() { switchToTab(2) })
	tab4Btn.SetSelectedFunc(func() { switchToTab(3) })

	tabBar := tview.NewFlex().
		AddItem(tab1Btn, 0, 1, true).
		AddItem(tab2Btn, 0, 1, true).
		AddItem(tab3Btn, 0, 1, true).
		AddItem(tab4Btn, 0, 1, true)

	// 主布局
	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabBar, 1, 0, true).
		AddItem(pages, 0, 1, true)

	return mainLayout
}

// newRulesListPage 创建规则列表页面
func newRulesListPage(app *tview.Application, pages *tview.Pages) tview.Primitive {
	allRules := []mihomotui.Rule{}
	filteredRules := []mihomotui.Rule{}

	currentPage := 0
	maxPerPage := 10

	inputField := tview.NewInputField().
		SetPlaceholder(" 过滤条件").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	inputField.SetBorder(true)

	countInfo := tview.NewTextView().
		SetTextAlign(tview.AlignRight).
		SetDynamicColors(true)

	toolbar := tview.NewFlex().
		AddItem(inputField, 0, 1, true).
		AddItem(countInfo, 16, 0, false)

	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetSeparator(' ')

	prevBtn := tview.NewButton(" < ")
	prevBtn.SetBorder(false)
	nextBtn := tview.NewButton(" > ")
	nextBtn.SetBorder(false)
	pageInfo := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	bottomBar := tview.NewFlex().
		AddItem(prevBtn, 5, 0, true).
		AddItem(pageInfo, 12, 0, false).
		AddItem(nextBtn, 5, 0, true)

	totalPages := func() int {
		n := len(filteredRules)
		if n == 0 {
			return 1
		}
		return (n + maxPerPage - 1) / maxPerPage
	}

	var refreshTable func()
	refreshTable = func() {
		table.Clear()
		tp := totalPages()
		if currentPage >= tp {
			currentPage = tp - 1
		}
		if currentPage < 0 {
			currentPage = 0
		}
		start := currentPage * maxPerPage
		end := min(start+maxPerPage, len(filteredRules))
		countInfo.SetText(fmt.Sprintf(" %d / %d ", len(filteredRules), len(allRules)))
		pageInfo.SetText(fmt.Sprintf(" %d / %d ", currentPage+1, tp))
		table.SetCell(0, 0, tview.NewTableCell(" # ").SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold).SetAlign(tview.AlignCenter))
		table.SetCell(0, 1, tview.NewTableCell(" 规则 ").SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold))
		table.SetCell(0, 2, tview.NewTableCell(" 类型 ").SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold))
		table.SetCell(0, 3, tview.NewTableCell(" 策略 ").SetTextColor(tcell.ColorYellow).SetAttributes(tcell.AttrBold))
		for i := start; i < end; i++ {
			r := filteredRules[i]
			row := i - start + 1
			idxCell := tview.NewTableCell(fmt.Sprintf(" %d ", i+1)).SetAlign(tview.AlignCenter)
			contentCell := tview.NewTableCell(" " + r.Content)
			typeCell := tview.NewTableCell(r.Type).SetTextColor(tcell.ColorGray)
			policyCell := tview.NewTableCell(r.Policy)
			switch r.Policy {
			case "DIRECT":
				policyCell.SetTextColor(tcell.ColorGreen)
			case "REJECT":
				policyCell.SetTextColor(tcell.ColorRed)
			default:
				policyCell.SetTextColor(tcell.ColorBlue)
			}
			table.SetCell(row, 0, idxCell)
			table.SetCell(row, 1, contentCell)
			table.SetCell(row, 2, typeCell)
			table.SetCell(row, 3, policyCell)
		}
		if len(filteredRules) == 0 {
			table.SetCell(1, 1, tview.NewTableCell(" 无匹配规则 ").SetTextColor(tcell.ColorGray))
		}
	}

	filter := func(keyword string) {
		lower := strings.ToLower(keyword)
		if lower == "" {
			filteredRules = make([]mihomotui.Rule, len(allRules))
			copy(filteredRules, allRules)
		} else {
			filteredRules = filteredRules[:0]
			for _, r := range allRules {
				if strings.Contains(strings.ToLower(r.Content), lower) ||
					strings.Contains(strings.ToLower(r.Type), lower) ||
					strings.Contains(strings.ToLower(r.Policy), lower) {
					filteredRules = append(filteredRules, r)
				}
			}
		}
		currentPage = 0
		refreshTable()
	}

	inputField.SetChangedFunc(func(text string) {
		filter(text)
	})

	prevBtn.SetSelectedFunc(func() {
		if currentPage > 0 {
			currentPage--
			refreshTable()
		}
	})

	nextBtn.SetSelectedFunc(func() {
		if currentPage < totalPages()-1 {
			currentPage++
			refreshTable()
		}
	})

	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, true).
		AddItem(table, 0, 1, true).
		AddItem(bottomBar, 1, 0, true)

	lastHeight := 0
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		available := max(height-5, 1)
		if height != lastHeight || available != maxPerPage {
			lastHeight = height
			maxPerPage = available
			refreshTable()
		}
		return x, y, width, height
	})

	go func() {
		load := func() {
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				return
			}
			rules, err := api.GetRulesParsed()
			if err != nil {
				return
			}
			allRules = rules
			filter(inputField.GetText())
		}
		load()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			load()
		}
	}()

	refreshTable()
	return page
}

// newRuleProviderPage 创建规则订阅页面
func newRuleProviderPage(app *tview.Application, pages *tview.Pages) tview.Primitive {
	cfg := mihomotui.GlobalConfig()

	showModal := func(title, message string) {
		modal := tview.NewModal().
			SetText(fmt.Sprintf("%s\n\n%s", title, message)).
			AddButtons([]string{"确认"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				pages.HidePage("modal")
				pages.RemovePage("modal")
			})
		pages.AddPage("modal", modal, true, true)
	}

	ruleProviders := []mihomotui.RuleProviderSubscription{}
	reloadRps := func() {
		ruleProviders = make([]mihomotui.RuleProviderSubscription, 0, len(cfg.RuleProviderSubscriptions))
		for _, meta := range cfg.RuleProviderSubscriptions {
			ruleProviders = append(ruleProviders, meta)
		}
	}
	reloadRps()

	currentPage := 0
	maxPerPage := 4
	cardHeight := 6
	selectedRp := 0

	urlInput := tview.NewInputField().
		SetPlaceholder(" 规则订阅链接 (https://...)").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	urlInput.SetBorder(true)

	behaviorDropdown := tview.NewDropDown().
		SetOptions([]string{"classical", "domain", "ipcidr"}, nil).
		SetCurrentOption(0)
	behaviorDropdown.SetBorder(true).SetTitle(" 类型 ")

	proxyGroupDropdown := tview.NewDropDown().
		SetOptions(mihomotui.PolicyList, nil).
		SetCurrentOption(0)
	proxyGroupDropdown.SetBorder(true).SetTitle(" 策略 ")

	importBtn := tview.NewButton(" 导入 ")
	importBtn.SetBorder(false)

	toolbar := tview.NewFlex().
		AddItem(urlInput, 0, 3, false).
		AddItem(behaviorDropdown, 14, 0, false).
		AddItem(proxyGroupDropdown, 14, 0, false).
		AddItem(importBtn, 10, 0, false)

	listFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	prevBtn := tview.NewButton(" < ")
	prevBtn.SetBorder(false)
	nextBtn := tview.NewButton(" > ")
	nextBtn.SetBorder(false)
	pageInfo := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")

	bottomBar := tview.NewFlex().
		AddItem(prevBtn, 5, 0, false).
		AddItem(pageInfo, 12, 0, false).
		AddItem(nextBtn, 5, 0, false).
		AddItem(statusBar, 0, 1, false)

	totalPages := func() int {
		if len(ruleProviders) == 0 {
			return 1
		}
		return (len(ruleProviders) + maxPerPage - 1) / maxPerPage
	}

	updatePager := func() {
		tp := totalPages()
		pageInfo.SetText(fmt.Sprintf(" %d / %d ", currentPage+1, tp))
	}

	refreshRp := func(idx int) {}
	deleteRp := func(idx int) {}
	refreshCards := func() {}

	refreshRp = func(idx int) {
		if idx < 0 || idx >= len(ruleProviders) {
			return
		}
		name := ruleProviders[idx].Name
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { showModal("刷新失败", err.Error()) })
				return
			}
			if err := client.IPCRefreshRuleProvider(name); err != nil {
				app.QueueUpdateDraw(func() { showModal("刷新失败", err.Error()) })
				return
			}
			cfg2, err := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if err != nil {
					showModal("刷新成功，但同步失败", err.Error())
					return
				}
				cfg = cfg2
				mihomotui.SyncConfigFromServer(cfg2)
				mihomotui.ResetMihomoAPI()
				reloadRps()
				refreshCards()
				showModal("刷新成功", fmt.Sprintf("已刷新规则订阅: %s", name))
			})
		}()
	}

	deleteRp = func(idx int) {
		if idx < 0 || idx >= len(ruleProviders) {
			return
		}
		name := ruleProviders[idx].Name
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("删除失败", err.Error())
				})
				return
			}
			if err := client.IPCDeleteRuleProvider(name); err != nil {
				app.QueueUpdateDraw(func() {
					showModal("删除失败", err.Error())
				})
				return
			}
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					cfg = cfg2
					mihomotui.SyncConfigFromServer(cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadRps()
				if selectedRp >= len(ruleProviders) && len(ruleProviders) > 0 {
					selectedRp = len(ruleProviders) - 1
				}
				refreshCards()
			})
		}()
	}

	refreshCards = func() {
		listFlex.Clear()
		tp := totalPages()
		if currentPage >= tp {
			currentPage = tp - 1
		}
		if currentPage < 0 {
			currentPage = 0
		}
		start := currentPage * maxPerPage
		end := min(start+maxPerPage, len(ruleProviders))
		for i := start; i < end; i++ {
			idx := i
			rp := &ruleProviders[idx]
			pg := rp.ProxyGroup
			if pg == "" {
				pg = "Auto"
			}
			statusText := "[green]上次刷新成功[-]"
			if rp.LastError != "" {
				statusText = fmt.Sprintf("[red]刷新失败: %s[-]", mihomotui.RedactURLInText(rp.LastError))
			}
			infoText := fmt.Sprintf(
				"[blue::b] %s[-:-:-]    行为: %s    格式: %s    间隔: %ds\n"+
					" 来源: %s    更新: %s    策略: %s  %s",
				rp.Name, rp.Behavior, rp.Format, rp.Interval,
				mihomotui.RedactURL(rp.URL), rp.UpdatedAt, pg, statusText,
			)
			info := tview.NewTextView().SetText(infoText).SetDynamicColors(true)
			refreshBtn := tview.NewButton(" ↻ ")
			refreshBtn.SetBorder(false)
			refreshBtn.SetSelectedFunc(func() {
				refreshRp(idx)
			})
			deleteBtn := tview.NewButton(" ✕ ")
			deleteBtn.SetBorder(false)
			deleteBtn.SetSelectedFunc(func() {
				deleteRp(idx)
			})
			policyBtn := tview.NewButton(" 策略 ")
			policyBtn.SetBorder(false)
			policyBtn.SetSelectedFunc(func() {
				si := cfg.FindRuleProviderByName(rp.Name)
				if si < 0 {
					return
				}
				currentPg := rp.ProxyGroup
				if currentPg == "" {
					currentPg = "Auto"
				}
				modal := tview.NewModal().
					SetText(fmt.Sprintf("选择策略组\n\n%s\n当前: %s", rp.Name, currentPg)).
					AddButtons([]string{"Auto", "DIRECT", "REJECT", "取消"}).
					SetDoneFunc(func(buttonIndex int, buttonLabel string) {
						pages.HidePage("policy_modal")
						pages.RemovePage("policy_modal")
						if buttonLabel == "取消" {
							return
						}
						go func() {
							// 基于服务端最新配置按名称定位规则订阅后提交单字段变更
							_, err := mihomotui.MutateServerConfig(func(fresh *mihomotui.Config) {
								if ri := fresh.FindRuleProviderByName(rp.Name); ri >= 0 {
									fresh.RuleProviderSubscriptions[ri].ProxyGroup = buttonLabel
								}
							})
							if err != nil {
								app.QueueUpdateDraw(func() {
									showModal("保存失败", err.Error())
								})
								return
							}
							app.QueueUpdateDraw(func() {
								reloadRps()
								refreshCards()
								showModal("修改成功", fmt.Sprintf("策略组已设置为: %s", buttonLabel))
							})
						}()
					})
				pages.AddPage("policy_modal", modal, true, true)
			})
			btnFlex := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(refreshBtn, 1, 0, true).
				AddItem(deleteBtn, 1, 0, true).
				AddItem(policyBtn, 1, 0, true)
			card := tview.NewFlex().
				AddItem(info, 0, 1, false).
				AddItem(btnFlex, 6, 0, true)
			card.SetBorder(true)
			if idx == selectedRp {
				card.SetBorderColor(tcell.ColorBlue)
				card.SetBorderAttributes(tcell.AttrBold)
			}
			listFlex.AddItem(card, cardHeight, 0, false)
		}
		if len(ruleProviders) == 0 {
			empty := tview.NewTextView().
				SetTextAlign(tview.AlignCenter).
				SetText("\n暂无规则订阅，请导入")
			listFlex.AddItem(empty, 0, 1, false)
		}
		updatePager()
	}

	prevBtn.SetSelectedFunc(func() {
		if currentPage > 0 {
			currentPage--
			refreshCards()
		}
	})

	nextBtn.SetSelectedFunc(func() {
		if currentPage < totalPages()-1 {
			currentPage++
			refreshCards()
		}
	})

	importBtn.SetSelectedFunc(func() {
		url := strings.TrimSpace(urlInput.GetText())
		if url == "" {
			showModal("导入失败", "请输入规则订阅链接")
			return
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			showModal("导入失败", "请输入以 http:// 或 https:// 开头的订阅链接")
			return
		}
		_, behavior := behaviorDropdown.GetCurrentOption()
		if behavior == "" {
			behavior = "classical"
		}
		_, proxyGroup := proxyGroupDropdown.GetCurrentOption()
		if proxyGroup == "" {
			proxyGroup = "Auto"
		}
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("导入失败", err.Error())
				})
				return
			}
			req := mihomotui.RuleProviderImportRequest{
				URL:        url,
				Behavior:   behavior,
				ProxyGroup: proxyGroup,
			}
			if err := client.IPCImportRuleProvider(req); err != nil {
				app.QueueUpdateDraw(func() {
					showModal("导入失败", err.Error())
				})
				return
			}
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					cfg = cfg2
					mihomotui.SyncConfigFromServer(cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadRps()
				urlInput.SetText("")
				currentPage = totalPages() - 1
				refreshCards()
				showModal("导入成功", fmt.Sprintf("成功导入规则订阅: %s", ruleProviders[len(ruleProviders)-1].Name))
			})
		}()
	})

	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(listFlex, 0, 1, false).
		AddItem(bottomBar, 1, 0, false)

	page.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() != page {
			return event
		}
		total := len(ruleProviders)
		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(urlInput)
			return nil
		case tcell.KeyDown:
			if selectedRp < total-1 {
				selectedRp++
				if selectedRp >= (currentPage+1)*maxPerPage {
					currentPage++
				}
				refreshCards()
			}
			return nil
		case tcell.KeyUp:
			if selectedRp > 0 {
				selectedRp--
				if selectedRp < currentPage*maxPerPage {
					currentPage--
				}
				refreshCards()
			}
			return nil
		case tcell.KeyEnter:
			if selectedRp >= 0 && selectedRp < total {
				refreshRp(selectedRp)
			}
			return nil
		}
		return event
	})

	lastHeight := 0
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		available := max(height-4, cardHeight)
		perPage := max(available/cardHeight, 1)
		if height != lastHeight || perPage != maxPerPage {
			lastHeight = height
			maxPerPage = perPage
			refreshCards()
		}
		statusBar.SetText(fmt.Sprintf(" 共%d条 每页%d条 ", len(ruleProviders), maxPerPage))
		return x, y, width, height
	})

	refreshCards()
	return page
}

// newCustomRulesPage manages custom rules on either side of built-in rules.
func newCustomRulesPage(app *tview.Application, pages *tview.Pages) tview.Primitive {
	input := tview.NewInputField().SetPlaceholder("DOMAIN,example.com 或 IP-CIDR,1.1.1.1/32,no-resolve")
	input.SetBorder(true).SetTitle("规则")
	policy := tview.NewDropDown().SetOptions(mihomotui.PolicyList, nil).SetCurrentOption(0)
	policy.SetBorder(true).SetTitle("策略")
	position := tview.NewDropDown().SetOptions([]string{"前置", "后置"}, nil).SetCurrentOption(0)
	position.SetBorder(true).SetTitle("位置")
	add := tview.NewButton("添加")
	list := tview.NewFlex().SetDirection(tview.FlexRow)
	refresh := func() {}
	commit := func(change func(*mihomotui.Config)) {
		go func() {
			_, err := mihomotui.MutateServerConfig(change)
			app.QueueUpdateDraw(func() {
				if err != nil {
					ShowAlertModal(app, pages, "保存失败", err.Error())
					return
				}
				refresh()
			})
		}()
	}
	refresh = func() {
		cfg := mihomotui.GlobalConfig()
		list.Clear()
		addGroup := func(title string, rules []string, post bool) {
			list.AddItem(tview.NewTextView().SetText("[::b] "+title).SetDynamicColors(true), 1, 0, false)
			for i, text := range rules {
				idx, rule := i, text
				info := tview.NewTextView().SetText(fmt.Sprintf(" %d. %s", i+1, text))
				up := tview.NewButton("↑")
				down := tview.NewButton("↓")
				del := tview.NewButton("✕")
				move := func(delta int) {
					commit(func(c *mihomotui.Config) {
						target := &c.PreCustomRules
						if post {
							target = &c.PostCustomRules
						}
						j := idx + delta
						if j >= 0 && j < len(*target) {
							(*target)[idx], (*target)[j] = (*target)[j], (*target)[idx]
						}
					})
				}
				up.SetSelectedFunc(func() { move(-1) })
				down.SetSelectedFunc(func() { move(1) })
				del.SetSelectedFunc(func() {
					ShowConfirmModal(app, pages, "确认删除", rule, func() {
						commit(func(c *mihomotui.Config) {
							target := &c.PreCustomRules
							if post {
								target = &c.PostCustomRules
							}
							for k, v := range *target {
								if v == rule {
									*target = append((*target)[:k], (*target)[k+1:]...)
									break
								}
							}
						})
					})
				})
				list.AddItem(tview.NewFlex().AddItem(info, 0, 1, false).AddItem(up, 3, 0, false).AddItem(down, 3, 0, false).AddItem(del, 3, 0, false), 1, 0, false)
			}
		}
		addGroup("前置规则", cfg.PreCustomRules, false)
		addGroup("后置规则", cfg.PostCustomRules, true)
	}
	add.SetSelectedFunc(func() {
		rule := strings.TrimSpace(input.GetText())
		if rule == "" {
			ShowAlertModal(app, pages, "添加失败", "请输入规则内容")
			return
		}
		if strings.HasPrefix(strings.ToUpper(rule), "MATCH,") {
			ShowAlertModal(app, pages, "添加失败", "自定义规则不能包含 MATCH")
			return
		}
		if !strings.Contains(rule, ",") {
			ShowAlertModal(app, pages, "添加失败", "规则格式不正确")
			return
		}
		_, pg := policy.GetCurrentOption()
		if !slices.Contains(mihomotui.PolicyList, strings.TrimSpace(rule[strings.LastIndex(rule, ",")+1:])) {
			rule += "," + pg
		}
		_, pos := position.GetCurrentOption()
		commit(func(c *mihomotui.Config) {
			if pos == "后置" {
				c.PostCustomRules = append(c.PostCustomRules, rule)
			} else {
				c.PreCustomRules = append(c.PreCustomRules, rule)
			}
		})
		input.SetText("")
	})
	refresh()
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(tview.NewFlex().AddItem(input, 0, 3, true).AddItem(policy, 14, 0, false).AddItem(position, 12, 0, false).AddItem(add, 8, 0, false), 3, 0, false).AddItem(list, 0, 1, false)
}

// newBuiltInRulesPage exposes enabled state, priority and editable built-in settings.
// Flex itself is not scrollable, so cards are explicitly paginated to keep every rule reachable.
func newBuiltInRulesPage(app *tview.Application, pages *tview.Pages) tview.Primitive {
	// 每个卡片占 4 行；实际每页数由页面可用高度在 Draw 时计算。
	perPage := 1
	lastHeight := 0
	list := tview.NewFlex().SetDirection(tview.FlexRow)
	restoreAll := tview.NewButton("恢复全部默认")
	prev := tview.NewButton("上一页")
	next := tview.NewButton("下一页")
	pageInfo := tview.NewTextView().SetTextAlign(tview.AlignCenter)
	currentPage := 0
	refresh := func() {}
	commit := func(change func(*mihomotui.Config)) {
		go func() {
			_, err := mihomotui.MutateServerConfig(change)
			app.QueueUpdateDraw(func() {
				if err != nil {
					ShowAlertModal(app, pages, "保存失败", err.Error())
					return
				}
				refresh()
			})
		}()
	}
	refresh = func() {
		cfg := mihomotui.GlobalConfig()
		totalPages := max((len(cfg.BuiltInRules)+perPage-1)/perPage, 1)
		if currentPage >= totalPages {
			currentPage = totalPages - 1
		}
		start, end := currentPage*perPage, min((currentPage+1)*perPage, len(cfg.BuiltInRules))
		list.Clear()
		for i := start; i < end; i++ {
			idx, e := i, cfg.BuiltInRules[i]
			state := "已启用"
			if !e.Enabled {
				state = "已禁用"
			}
			summary := e.Rule
			if e.Kind == mihomotui.BuiltInRuleProvider {
				summary = fmt.Sprintf("%s → %s", e.URL, e.ProxyGroup)
			}
			if e.Kind == mihomotui.BuiltInRuleMatch {
				summary = "MATCH," + e.ProxyGroup
			}
			text := tview.NewTextView().SetDynamicColors(true).SetText(fmt.Sprintf("[%s] %s · %s\n%s", state, e.Name, e.Kind, summary))
			toggle, edit, reset := tview.NewButton("启用/禁用"), tview.NewButton("编辑"), tview.NewButton("恢复")
			up, down := tview.NewButton("↑"), tview.NewButton("↓")
			if e.Kind == mihomotui.BuiltInRuleMatch {
				toggle.SetDisabled(true)
				up.SetDisabled(true)
				down.SetDisabled(true)
			}
			toggle.SetSelectedFunc(func() {
				commit(func(c *mihomotui.Config) { c.BuiltInRules[idx].Enabled = !c.BuiltInRules[idx].Enabled })
			})
			reset.SetSelectedFunc(func() {
				commit(func(c *mihomotui.Config) {
					for _, v := range mihomotui.DefaultBuiltInRules() {
						if v.ID == e.ID {
							c.BuiltInRules[idx] = v
							break
						}
					}
					for n := range c.BuiltInRules {
						c.BuiltInRules[n].Order = n
					}
				})
			})
			move := func(delta int) {
				commit(func(c *mihomotui.Config) {
					j := idx + delta
					if j >= 0 && j < len(c.BuiltInRules)-1 {
						c.BuiltInRules[idx], c.BuiltInRules[j] = c.BuiltInRules[j], c.BuiltInRules[idx]
						for n := range c.BuiltInRules {
							c.BuiltInRules[n].Order = n
						}
					}
				})
			}
			up.SetSelectedFunc(func() { move(-1) })
			down.SetSelectedFunc(func() { move(1) })
			edit.SetSelectedFunc(func() {
				showBuiltInRuleEditor(app, pages, e, func(updated mihomotui.BuiltInRule) {
					commit(func(c *mihomotui.Config) { c.BuiltInRules[idx] = updated })
				})
			})
			buttons := tview.NewFlex().AddItem(toggle, 10, 0, false).AddItem(edit, 6, 0, false).AddItem(up, 3, 0, false).AddItem(down, 3, 0, false).AddItem(reset, 6, 0, false)
			card := tview.NewFlex().AddItem(text, 0, 1, false).AddItem(buttons, 30, 0, false)
			card.SetBorder(true)
			list.AddItem(card, 4, 0, false)
		}
		if len(cfg.BuiltInRules) == 0 {
			list.AddItem(tview.NewTextView().SetText("暂无内置规则，请使用“恢复全部默认”初始化。"), 0, 1, false)
		}
		pageInfo.SetText(fmt.Sprintf("第 %d / %d 页 · 共 %d 条", currentPage+1, totalPages, len(cfg.BuiltInRules)))
		prev.SetDisabled(currentPage == 0)
		next.SetDisabled(currentPage >= totalPages-1)
	}
	prev.SetSelectedFunc(func() {
		if currentPage > 0 {
			currentPage--
			refresh()
		}
	})
	next.SetSelectedFunc(func() { currentPage++; refresh() })
	restoreAll.SetSelectedFunc(func() {
		ShowConfirmModal(app, pages, "恢复全部默认", "将重置所有内置规则，保留自定义规则。", func() {
			commit(func(c *mihomotui.Config) {
				c.BuiltInRules = mihomotui.DefaultBuiltInRules()
				c.BuiltInRulesInitialized = true
			})
			currentPage = 0
		})
	})
	refresh()
	toolbar := tview.NewFlex().AddItem(restoreAll, 14, 0, true).AddItem(prev, 10, 0, false).AddItem(pageInfo, 0, 1, false).AddItem(next, 10, 0, false)
	page := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(toolbar, 1, 0, true).AddItem(list, 0, 1, false)
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// 减去工具栏后，以完整卡片为单位分页；窗口尺寸变化后保持当前首条规则可见。
		available := max(height-1, 4)
		newPerPage := max(available/4, 1)
		if newPerPage != perPage || height != lastHeight {
			first := currentPage * perPage
			perPage, lastHeight = newPerPage, height
			currentPage = first / perPage
			refresh()
		}
		return x, y, width, height
	})
	return page
}

func showBuiltInRuleEditor(app *tview.Application, pages *tview.Pages, entry mihomotui.BuiltInRule, done func(mihomotui.BuiltInRule)) {
	form := tview.NewForm()
	content := tview.NewInputField().SetLabel("规则 ").SetText(entry.Rule)
	url := tview.NewInputField().SetLabel("URL ").SetText(entry.URL)
	behavior := tview.NewInputField().SetLabel("行为 ").SetText(entry.Behavior)
	format := tview.NewInputField().SetLabel("格式 ").SetText(entry.Format)
	interval := tview.NewInputField().SetLabel("间隔(秒) ").SetText(fmt.Sprintf("%d", entry.Interval))
	group := tview.NewInputField().SetLabel("策略 ").SetText(entry.ProxyGroup)
	if entry.Kind == mihomotui.BuiltInRuleLiteral {
		form.AddFormItem(content)
	} else if entry.Kind == mihomotui.BuiltInRuleProvider {
		form.AddFormItem(url).AddFormItem(behavior).AddFormItem(format).AddFormItem(interval)
	}
	form.AddFormItem(group)
	form.AddButton("保存", func() {
		entry.Rule, entry.URL, entry.Behavior, entry.Format, entry.ProxyGroup = content.GetText(), url.GetText(), behavior.GetText(), format.GetText(), group.GetText()
		if _, err := fmt.Sscanf(interval.GetText(), "%d", &entry.Interval); entry.Kind == mihomotui.BuiltInRuleProvider && err != nil {
			ShowAlertModal(app, pages, "输入错误", "更新间隔必须是正整数")
			return
		}
		pages.RemovePage("builtin_editor")
		done(entry)
	}).AddButton("取消", func() { pages.RemovePage("builtin_editor") })
	form.SetBorder(true).SetTitle("编辑内置规则")
	pages.RemovePage("builtin_editor")
	pages.AddPage("builtin_editor", form, true, true)
	app.SetFocus(form)
}
