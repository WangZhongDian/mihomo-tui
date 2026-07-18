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
	status := tview.NewTextView().SetDynamicColors(true)
	add := tview.NewButton(" 新建订阅池 ")
	refresh := tview.NewButton(" 刷新列表 ")
	toolbar := tview.NewFlex().AddItem(add, 16, 0, true).AddItem(refresh, 14, 0, false).AddItem(status, 0, 1, false)
	base := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(toolbar, 3, 0, false).AddItem(list, 0, 1, true)
	pages.AddPage("base", base, true, true)

	var cfg *mihomotui.Config
	var render func()
	showModal := func(title, text string) {
		modal := tview.NewModal().SetText(title + "\n\n" + text).AddButtons([]string{"确认"}).SetDoneFunc(func(int, string) { pages.RemovePage("modal"); app.SetFocus(base) })
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
			app.QueueUpdateDraw(func() {
				if err == nil {
					cfg = fresh
					mihomotui.SyncConfigFromServer(fresh)
				}
				done(err)
			})
		}()
	}

	poolRequest := func(pool mihomotui.SubscriptionPool, members []string, active string, name string, enabled bool, interval int) mihomotui.SubscriptionPoolRequest {
		return mihomotui.SubscriptionPoolRequest{Name: name, Members: members, ActiveMemberID: active, Enabled: enabled, RefreshInterval: interval}
	}
	var showEditor func(existing *mihomotui.SubscriptionPool)
	showEditor = func(existing *mihomotui.SubscriptionPool) {
		if cfg == nil {
			showModal("无法编辑", "订阅数据尚未加载")
			return
		}
		name, enabled, interval := "新订阅池", true, 3600
		members := []string{}
		active := ""
		if existing != nil {
			name = existing.Name
			enabled = existing.Enabled
			interval = existing.RefreshInterval
			members = append(members, existing.Members...)
			active = existing.ActiveMemberID
		}
		form := tview.NewForm()
		nameField := tview.NewInputField().SetLabel("名称: ").SetText(name)
		intervalField := tview.NewInputField().SetLabel("刷新秒数: ").SetText(strconv.Itoa(interval))
		enabledBox := tview.NewCheckbox().SetLabel("启用此订阅池").SetChecked(enabled)
		form.AddFormItem(nameField).AddFormItem(intervalField).AddFormItem(enabledBox)
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
			for _, id := range members {
				prefix := "  "
				if id == active {
					prefix = "✓ "
				}
				memberList.AddItem(prefix+memberName(id), "", 0, nil)
			}
		}
		rebuildMembers()
		selector := tview.NewDropDown().SetLabel("添加成员: ")
		options, optionIDs := []string{"请选择订阅源"}, []string{""}
		for _, sub := range cfg.Subscriptions {
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
			for _, x := range members {
				if x == id {
					return
				}
			}
			members = append(members, id)
			if active == "" {
				active = id
			}
			rebuildMembers()
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
			}
		})
		buttons := tview.NewFlex().AddItem(addMember, 8, 0, false).AddItem(up, 8, 0, false).AddItem(down, 8, 0, false).AddItem(activate, 12, 0, false).AddItem(remove, 8, 0, false)
		right := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 8, 0, true).AddItem(selector, 1, 0, false).AddItem(buttons, 1, 0, false).AddItem(memberList, 0, 1, false)
		close := func() { pages.RemovePage("pool-editor"); app.SetFocus(base) }
		form.AddButton("保存", func() {
			parsed, err := strconv.Atoi(strings.TrimSpace(intervalField.GetText()))
			if err != nil || parsed <= 0 {
				showModal("保存失败", "刷新间隔必须是正整数秒")
				return
			}
			if enabledBox.IsChecked() && len(members) == 0 {
				showModal("保存失败", "启用的订阅池至少需要一个成员")
				return
			}
			req := poolRequest(mihomotui.SubscriptionPool{}, members, active, strings.TrimSpace(nameField.GetText()), enabledBox.IsChecked(), parsed)
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
		form.AddButton("取消", close)
		editor := tview.NewFlex().AddItem(right, 0, 1, true).SetBorder(true).SetTitle("订阅池编辑")
		pages.RemovePage("pool-editor")
		pages.AddPage("pool-editor", editor, true, true)
		app.SetFocus(nameField)
	}

	render = func() {
		list.Clear()
		if cfg == nil || len(cfg.SubscriptionPools) == 0 {
			list.AddItem(tview.NewTextView().SetText("\n暂无订阅池。请新建池并添加订阅源。"), 0, 1, false)
			return
		}
		for i := range cfg.SubscriptionPools {
			pool := cfg.SubscriptionPools[i]
			activeName := "无"
			cache, statusText := "无", "[yellow]已禁用[-]"
			if si := cfg.FindSubscriptionByID(pool.ActiveMemberID); si >= 0 {
				s := cfg.Subscriptions[si]
				activeName = s.Name
				if s.CacheFile != "" {
					cache = "可用"
				} else {
					cache = "缺失"
				}
				statusText = "[green]正常[-]"
				if s.FailureCount > 0 {
					statusText = fmt.Sprintf("[yellow]连续失败 %d[-]", s.FailureCount)
				}
			}
			if pool.Degraded {
				statusText = "[red]已降级[-]"
			}
			info := tview.NewTextView().SetDynamicColors(true).SetText(fmt.Sprintf("[yellow::b]%s[-:-:-]  %s\n活动源: %s  缓存: %s  成员: %d  间隔: %ds\n%s  切换: %s %s", pool.Name, statusText, activeName, cache, len(pool.Members), pool.RefreshInterval, strings.TrimSpace(pool.LastSwitchReason), pool.LastSwitchAt, ""))
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
			buttons := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(edit, 1, 0, true).AddItem(flush, 1, 0, false).AddItem(deleteBtn, 1, 0, false)
			card := tview.NewFlex().AddItem(info, 0, 1, false).AddItem(buttons, 8, 0, true).SetBorder(true)
			list.AddItem(card, 6, 0, false)
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
	return pages
}
