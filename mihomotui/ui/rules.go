package ui

import (
	"fmt"
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

	activeTab := 0 // 0 = 规则列表, 1 = 规则订阅

	// ===== 规则列表页面 =====
	rulesListPage := newRulesListPage(app, pages)

	// ===== 规则订阅页面 =====
	ruleProviderPage := newRuleProviderPage(app, pages)

	pages.AddPage("rules_list", rulesListPage, true, true)
	pages.AddPage("rule_providers", ruleProviderPage, true, false)

	// ===== Tab 栏（两个 Button 水平排列）=====
	tab1Btn := tview.NewButton(" 规则列表 ")
	tab1Btn.SetBorder(false)
	tab2Btn := tview.NewButton(" 规则订阅 ")
	tab2Btn.SetBorder(false)

	updateTabHighlight := func() {
		if activeTab == 0 {
			tab1Btn.SetLabel("[规则列表]")
			tab2Btn.SetLabel(" 规则订阅 ")
		} else {
			tab1Btn.SetLabel(" 规则列表 ")
			tab2Btn.SetLabel("[规则订阅]")
		}
	}
	updateTabHighlight()

	switchToTab := func(tab int) {
		if tab == activeTab {
			return
		}
		activeTab = tab
		updateTabHighlight()
		if activeTab == 0 {
			pages.SwitchToPage("rules_list")
			app.SetFocus(rulesListPage)
		} else {
			pages.SwitchToPage("rule_providers")
			app.SetFocus(ruleProviderPage)
		}
	}

	tab1Btn.SetSelectedFunc(func() { switchToTab(0) })
	tab2Btn.SetSelectedFunc(func() { switchToTab(1) })

	tabBar := tview.NewFlex().
		AddItem(tab1Btn, 0, 1, true).
		AddItem(tab2Btn, 0, 1, true)

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
		SetOptions([]string{"Auto", "DIRECT", "REJECT"}, nil).
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
		ruleProviders[idx].UpdatedAt = time.Now().Format("2006-01-02 15:04")
		si := cfg.FindRuleProviderByName(ruleProviders[idx].Name)
		if si >= 0 {
			cfg.RuleProviderSubscriptions[si].UpdatedAt = ruleProviders[idx].UpdatedAt
			_ = cfg.Flush()
		}
		refreshCards()
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
					mihomotui.SetGlobalConfig(*cfg2)
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
			infoText := fmt.Sprintf(
				"[blue::b] %s[-:-:-]    行为: %s    格式: %s    间隔: %ds\n"+
					" 来源: %s    更新: %s    策略: %s",
				rp.Name, rp.Behavior, rp.Format, rp.Interval,
				rp.URL, rp.UpdatedAt, pg,
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
							client, err := mihomotui.GetIPCClient()
							if err != nil {
								app.QueueUpdateDraw(func() {
									showModal("修改失败", err.Error())
								})
								return
							}
							cfg.RuleProviderSubscriptions[si].ProxyGroup = buttonLabel
							if err := cfg.Flush(); err != nil {
								app.QueueUpdateDraw(func() {
									showModal("保存失败", err.Error())
								})
								return
							}
							if err := client.IPCUpdateConfig(cfg); err != nil {
								app.QueueUpdateDraw(func() {
									showModal("同步失败", err.Error())
								})
								return
							}
							cfg2, _ := client.IPCGetConfig()
							app.QueueUpdateDraw(func() {
								if cfg2 != nil {
									mihomotui.SetGlobalConfig(*cfg2)
								}
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
					mihomotui.SetGlobalConfig(*cfg2)
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
