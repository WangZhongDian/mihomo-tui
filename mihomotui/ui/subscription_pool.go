package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// NewSubscriptionPoolPage 提供订阅池、主备顺序及活动源的专门管理界面。
func NewSubscriptionPoolPage(app *tview.Application) tview.Primitive {
	pages := tview.NewPages()
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	list := tview.NewFlex().SetDirection(tview.FlexRow)
	list.SetBorder(true).SetTitle("订阅池列表")
	status := tview.NewTextView().SetDynamicColors(true)
	add := tview.NewButton(" 新建订阅池 ")
	refresh := tview.NewButton(" 刷新列表 ")
	toolbar := tview.NewFlex().AddItem(add, 16, 0, true).AddItem(refresh, 14, 0, false).AddItem(status, 0, 1, false)
	base := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(toolbar, 3, 0, false).AddItem(list, 0, 1, true)
	pages.AddPage("base", base, true, true)

	var cfg *mihomotui.Config
	var render func()
	showModal := func(title, text string) {
		modal := tview.NewModal().SetText(title + "\n\n" + text).AddButtons([]string{"确认"}).SetDoneFunc(func(int, string) { pages.RemovePage("modal"); app.SetFocus(pages); app.SetFocus(add) })
		pages.RemovePage("modal")
		pages.AddPage("modal", modal, true, true)
		app.SetFocus(modal)
	}
	load := func(done func(error)) {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { done(err) })
				return
			}
			fresh, err := client.IPCGetConfig()
			if err == nil {
				pools, poolErr := client.IPCGetSubscriptionPools()
				if poolErr != nil {
					err = poolErr
				} else {
					fresh.SubscriptionPools = pools
				}
			}
			app.QueueUpdateDraw(func() {
				if err == nil {
					cfg = fresh
					mihomotui.SyncConfigFromServer(fresh)
				}
				done(err)
			})
		}()
	}

	poolRequest := func(members []string, active string, name string, mode mihomotui.SubscriptionPoolMode, enabled bool, interval int) mihomotui.SubscriptionPoolRequest {
		return mihomotui.SubscriptionPoolRequest{Name: name, Mode: mode, Members: members, ActiveMemberID: active, Enabled: enabled, RefreshInterval: interval}
	}
	var showEditor func(existing *mihomotui.SubscriptionPool)
	showEditor = func(existing *mihomotui.SubscriptionPool) {
		if cfg == nil {
			showModal("无法编辑", "订阅数据尚未加载")
			return
		}
		var nameField *tview.InputField
		ShowOverlay(app, pages, "pool-editor", "订阅池编辑（Tab 切换控件，Esc 返回列表）", func(close func()) (tview.Primitive, tview.Primitive) {
			name, enabled, interval := "新订阅池", true, 3600
			mode := mihomotui.SubscriptionPoolModeFailover
			members := []string{}
			active := ""
			if existing != nil {
				name = existing.Name
				enabled = existing.Enabled
				interval = existing.RefreshInterval
				mode = existing.Mode
				if mode == "" {
					mode = mihomotui.SubscriptionPoolModeFailover
				}
				members = append(members, existing.Members...)
				active = existing.ActiveMemberID
			}
			form := tview.NewForm().SetButtonsAlign(tview.AlignRight)
			nameField = tview.NewInputField().SetLabel("名称: ").SetText(name)
			modeDropdown := tview.NewDropDown().SetLabel("运行模式: ")
			modeOptions := []string{"优先级主备（仅活动源）", "合并（全部缓存成员）"}
			modeIndex := 0
			if mode == mihomotui.SubscriptionPoolModeMerge {
				modeIndex = 1
			}
			modeDropdown.SetOptions(modeOptions, func(_ string, index int) {
				if index == 1 {
					mode = mihomotui.SubscriptionPoolModeMerge
				} else {
					mode = mihomotui.SubscriptionPoolModeFailover
				}
			}).SetCurrentOption(modeIndex)
			intervalField := tview.NewInputField().SetLabel("刷新秒数: ").SetText(strconv.Itoa(interval))
			enabledBox := tview.NewCheckbox().SetLabel("启用此订阅池").SetChecked(enabled)
			form.AddFormItem(nameField).AddFormItem(modeDropdown).AddFormItem(intervalField).AddFormItem(enabledBox)
			memberList := tview.NewList().ShowSecondaryText(false)
			memberList.SetBorder(true).SetTitle("成员顺序（上方优先）")
			var rebuildMembers func()
			memberName := func(id string) string {
				if i := cfg.FindSubscriptionByID(id); i >= 0 {
					return cfg.Subscriptions[i].Name
				}
				return id
			}
			rebuildMembers = func() {
				memberList.Clear()
				for i, id := range members {
					name := memberName(id)
					if si := cfg.FindSubscriptionByID(id); si >= 0 && cfg.Subscriptions[si].Disabled {
						name += " [gray](已禁用)[-]"
					}
					memberList.AddItem(fmt.Sprintf("%d. %s", i+1, name), "", 0, nil)
				}
			}
			rebuildMembers()
			selector := tview.NewDropDown().SetLabel("添加成员: ")
			// 下拉菜单只显示尚未加入此池的订阅。
			options, optionIDs := []string{"请选择订阅源"}, []string{""}
			for _, sub := range cfg.Subscriptions {
				alreadyInPool := false
				for _, id := range members {
					if id == sub.ID {
						alreadyInPool = true
						break
					}
				}
				if alreadyInPool {
					continue
				}
				options = append(options, sub.Name)
				optionIDs = append(optionIDs, sub.ID)
			}
			selector.SetOptions(options, nil)
			addMember := tview.NewButton("添加").SetSelectedFunc(func() {
				idx, _ := selector.GetCurrentOption()
				if idx <= 0 {
					return
				}
				id := optionIDs[idx]
				members = append(members, id)
				if active == "" {
					active = id
				}
				rebuildMembers()
				// 添加后刷新下拉菜单，把新成员从选项里移除。
				newOptions, newOptionIDs := []string{"请选择订阅源"}, []string{""}
				for _, sub := range cfg.Subscriptions {
					inPool := false
					for _, mid := range members {
						if mid == sub.ID {
							inPool = true
							break
						}
					}
					if !inPool {
						newOptions = append(newOptions, sub.Name)
						newOptionIDs = append(newOptionIDs, sub.ID)
					}
				}
				options, optionIDs = newOptions, newOptionIDs
				selector.SetOptions(options, nil)
				selector.SetCurrentOption(0)
			})
			up := tview.NewButton("上移").SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i > 0 && i < len(members) {
					members[i-1], members[i] = members[i], members[i-1]
					rebuildMembers()
					memberList.SetCurrentItem(i - 1)
				}
			})
			down := tview.NewButton("下移").SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i >= 0 && i < len(members)-1 {
					members[i+1], members[i] = members[i], members[i+1]
					rebuildMembers()
					memberList.SetCurrentItem(i + 1)
				}
			})
			activate := tview.NewButton("设为活动").SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i >= 0 && i < len(members) {
					active = members[i]
					rebuildMembers()
				}
			})
			remove := tview.NewButton("移除").SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i >= 0 && i < len(members) {
					removed := members[i]
					members = append(members[:i], members[i+1:]...)
					if active == removed {
						active = ""
						if len(members) > 0 {
							active = members[0]
						}
					}
					rebuildMembers()
					// 移除后刷新下拉菜单，把该订阅加回选项。
					newOptions, newOptionIDs := []string{"请选择订阅源"}, []string{""}
					for _, sub := range cfg.Subscriptions {
						inPool := false
						for _, mid := range members {
							if mid == sub.ID {
								inPool = true
								break
							}
						}
						if !inPool {
							newOptions = append(newOptions, sub.Name)
							newOptionIDs = append(newOptionIDs, sub.ID)
						}
					}
					options, optionIDs = newOptions, newOptionIDs
					selector.SetOptions(options, nil)
					selector.SetCurrentOption(0)
				}
			})
			toggleDisable := tview.NewButton("禁用/启用").SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i < 0 || i >= len(members) {
					return
				}
				id := members[i]
				si := cfg.FindSubscriptionByID(id)
				if si < 0 {
					return
				}
				newDisabled := !cfg.Subscriptions[si].Disabled
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err == nil {
						sub := cfg.Subscriptions[si]
						disabled := newDisabled
						err = client.IPCUpdateSubscription(sub.ID, mihomotui.SubscriptionUpdateRequest{
							Name:               sub.Name,
							URL:                sub.URL,
							UseLocalProxy:      sub.UseLocalProxy,
							UserAgent:          sub.UserAgent,
							FetchProxyStrategy: sub.FetchProxyStrategy,
							Disabled:           &disabled,
						})
					}
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal("切换失败", err.Error())
							return
						}
						cfg.Subscriptions[si].Disabled = newDisabled
						rebuildMembers()
					})
				}()
			})
			memberActions := tview.NewFlex().
				AddItem(addMember, 8, 0, false).
				AddItem(nil, 1, 0, false).
				AddItem(up, 8, 0, false).
				AddItem(nil, 1, 0, false).
				AddItem(down, 8, 0, false).
				AddItem(nil, 1, 0, false).
				AddItem(activate, 12, 0, false).
				AddItem(nil, 1, 0, false).
				AddItem(remove, 8, 0, false).
				AddItem(nil, 1, 0, false).
				AddItem(toggleDisable, 12, 0, false)
			save := tview.NewButton(" 保存 ")
			cancel := tview.NewButton(" 取消 ").SetSelectedFunc(close)
			save.SetSelectedFunc(func() {
				parsed, err := strconv.Atoi(strings.TrimSpace(intervalField.GetText()))
				if err != nil || parsed <= 0 {
					showModal("保存失败", "刷新间隔必须是正整数秒")
					return
				}
				if enabledBox.IsChecked() && len(members) == 0 {
					showModal("保存失败", "启用的订阅池至少需要一个成员")
					return
				}
				req := poolRequest(members, active, strings.TrimSpace(nameField.GetText()), mode, enabledBox.IsChecked(), parsed)
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err == nil {
						if existing == nil {
							_, err = client.IPCCreateSubscriptionPool(req)
						} else {
							err = client.IPCUpdateSubscriptionPool(existing.ID, req)
						}
					}
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal("保存失败", err.Error())
							return
						}
						close()
						load(func(e error) {
							if e != nil {
								showModal("同步失败", e.Error())
							} else {
								render()
							}
						})
					})
				}()
			})
			actionBar := tview.NewFlex().SetDirection(tview.FlexColumn).AddItem(nil, 0, 1, false).AddItem(save, 12, 0, true).AddItem(cancel, 12, 0, false)
			right := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(form, 6, 0, true).
				AddItem(selector, 1, 0, false).
				AddItem(memberActions, 1, 0, false).
				AddItem(memberList, 0, 1, false).
				AddItem(actionBar, 1, 0, false)
			editor := tview.NewFlex().AddItem(right, 0, 1, true)
			return editor, nameField
		})
	}

	render = func() {
		list.Clear()
		if cfg == nil || len(cfg.SubscriptionPools) == 0 {
			empty := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText("\n暂无订阅池。\n\n请点击上方“新建订阅池”创建主备集合。")
			list.AddItem(empty, 0, 1, false)
			return
		}
		for i := range cfg.SubscriptionPools {
			pool := cfg.SubscriptionPools[i]
			activeName, cache := "无", "无"
			state := "[yellow]已禁用[-]"
			if si := cfg.FindSubscriptionByID(pool.ActiveMemberID); si >= 0 {
				sub := cfg.Subscriptions[si]
				activeName = sub.Name
				if sub.CacheFile != "" {
					cache = "可用"
				} else {
					cache = "缺失"
				}
				state = "[green]正常[-]"
				if sub.FailureCount > 0 {
					state = fmt.Sprintf("[yellow]连续失败 %d[-]", sub.FailureCount)
				}
			}
			if pool.Degraded {
				state = "[red]已降级[-]"
			}
			reason := strings.TrimSpace(pool.LastSwitchReason)
			if reason == "" {
				reason = "无"
			}
			modeText := "优先级主备"
			if pool.Mode == mihomotui.SubscriptionPoolModeMerge {
				modeText = "合并"
				activeName = "全部可用成员"
				available := 0
				for _, memberID := range pool.Members {
					if si := cfg.FindSubscriptionByID(memberID); si >= 0 && cfg.Subscriptions[si].CacheFile != "" {
						available++
					}
				}
				cache = fmt.Sprintf("%d/%d 可用", available, len(pool.Members))
			}
			content := fmt.Sprintf("[yellow::b]%s[-:-:-]  %s\n运行模式: %s    活动源: %s    本地缓存: %s\n成员: %d    刷新间隔: %d 秒\n最近切换: %s\n原因: %s", pool.Name, state, modeText, activeName, cache, len(pool.Members), pool.RefreshInterval, pool.LastSwitchAt, reason)
			info := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetText(content)
			p := pool
			edit := tview.NewButton("编辑").SetSelectedFunc(func() { showEditor(&p) })
			flush := tview.NewButton("刷新").SetSelectedFunc(func() {
				go func() {
					c, e := mihomotui.GetIPCClient()
					if e == nil {
						e = c.IPCRefreshSubscriptionPool(p.ID)
					}
					app.QueueUpdateDraw(func() {
						if e != nil {
							showModal("刷新失败", e.Error())
							return
						}
						load(func(err error) {
							if err != nil {
								showModal("同步失败", err.Error())
							} else {
								render()
							}
						})
					})
				}()
			})
			deleteBtn := tview.NewButton("删除").SetSelectedFunc(func() {
				go func() {
					c, e := mihomotui.GetIPCClient()
					if e == nil {
						e = c.IPCDeleteSubscriptionPool(p.ID)
					}
					app.QueueUpdateDraw(func() {
						if e != nil {
							showModal("删除失败", e.Error())
							return
						}
						load(func(err error) {
							if err != nil {
								showModal("同步失败", err.Error())
							} else {
								render()
							}
						})
					})
				}()
			})
			buttonRow := tview.NewFlex().SetDirection(tview.FlexColumn).
				AddItem(edit, 8, 0, true).
				AddItem(nil, 2, 0, false).
				AddItem(flush, 8, 0, false).
				AddItem(nil, 2, 0, false).
				AddItem(deleteBtn, 8, 0, false)
			card := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(info, 0, 1, false).AddItem(buttonRow, 1, 0, true)
			card.SetBorder(true)
			list.AddItem(card, 8, 0, false)
		}
	}
	add.SetSelectedFunc(func() { showEditor(nil) })
	refresh.SetSelectedFunc(func() {
		load(func(err error) {
			if err != nil {
				showModal("刷新失败", err.Error())
				return
			}
			render()
		})
	})
	load(func(err error) {
		if err != nil {
			status.SetText("[red]加载失败: " + err.Error() + "[-]")
			return
		}
		status.SetText("")
		render()
	})
	return &poolPage{Primitive: pages, reload: func() {
		load(func(err error) {
			if err == nil {
				render()
			}
		})
	}}
}

// poolPage 包装订阅池页面，实现 Shower 接口：每次切换到该页面时自动刷新，
// 避免订阅删除后池成员显示过期的缓存数据。
type poolPage struct {
	tview.Primitive
	reload func()
}

func (p *poolPage) OnShow() { p.reload() }
