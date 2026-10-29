package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// applyFailureText 把"已保存但运行时未生效"的原因转成面向用户的说明。
func applyFailureText(report *mihomotui.SubscriptionPoolUpdateResponse) string {
	reason := strings.TrimSpace(report.ApplyError)
	if reason == "" {
		reason = T("pools.runtime_config_generation_failed")
	}
	stage := strings.TrimSpace(report.ApplyStage)
	if stage == "" {
		stage = "unknown"
	}
	return Tf("pools.apply_failure", map[string]any{"Stage": stage, "Reason": reason})
}

// NewSubscriptionPoolPage 提供订阅池、主备顺序及活动源的专门管理界面。
func NewSubscriptionPoolPage(app *tview.Application) tview.Primitive {
	pages := tview.NewPages()
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	list := tview.NewFlex().SetDirection(tview.FlexRow)
	list.SetBorder(true).SetTitle(T("pools.list_title"))
	status := tview.NewTextView().SetDynamicColors(true)
	add := tview.NewButton(T("pools.new_button"))
	refresh := tview.NewButton(T("pools.refresh_list_button"))
	toolbar := tview.NewFlex().AddItem(add, 16, 0, true).AddItem(refresh, 14, 0, false).AddItem(status, 0, 1, false)
	base := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(toolbar, 3, 0, false).AddItem(list, 0, 1, true)
	pages.AddPage("base", base, true, true)

	var cfg *mihomotui.Config
	var render func()
	showModal := func(title, text string) {
		modal := tview.NewModal().SetText(title + "\n\n" + text).AddButtons([]string{T("common.confirm")}).SetDoneFunc(func(int, string) { pages.RemovePage("modal"); app.SetFocus(pages); app.SetFocus(add) })
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
			showModal(T("pools.edit_unavailable"), T("pools.data_not_loaded"))
			return
		}
		var nameField *tview.InputField
		ShowOverlay(app, pages, "pool-editor", T("pools.editor_title"), func(close func()) (tview.Primitive, tview.Primitive) {
			name, enabled, interval := T("pools.new_default_name"), true, 3600
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
			nameField = tview.NewInputField().SetLabel(T("pools.name_label")).SetText(name)
			modeDropdown := tview.NewDropDown().SetLabel(T("pools.mode_label"))
			modeOptions := []string{T("pools.mode_failover_option"), T("pools.mode_merge_option")}
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
			intervalField := tview.NewInputField().SetLabel(T("pools.refresh_seconds_label")).SetText(strconv.Itoa(interval))
			enabledBox := tview.NewCheckbox().SetLabel(T("pools.enable_label")).SetChecked(enabled)
			form.AddFormItem(nameField).AddFormItem(modeDropdown).AddFormItem(intervalField).AddFormItem(enabledBox)
			memberList := tview.NewList().ShowSecondaryText(false)
			memberList.SetBorder(true).SetTitle(T("pools.member_order_title"))
			var rebuildMembers func()
			memberName := func(id string) string {
				// 仅用于展示：名称可能含国旗 emoji 或方括号标签语法。
				if i := cfg.FindSubscriptionByID(id); i >= 0 {
					return SanitizeDisplayText(cfg.Subscriptions[i].Name)
				}
				return SanitizeDisplayText(id)
			}
			rebuildMembers = func() {
				memberList.Clear()
				for i, id := range members {
					name := memberName(id)
					if si := cfg.FindSubscriptionByID(id); si >= 0 && cfg.Subscriptions[si].Disabled {
						name += T("pools.member_disabled_suffix")
					}
					memberList.AddItem(fmt.Sprintf("%d. %s", i+1, name), "", 0, nil)
				}
			}
			rebuildMembers()
			selector := tview.NewDropDown().SetLabel(T("pools.add_member_label"))
			// 下拉菜单只显示尚未加入此池的订阅。
			options, optionIDs := []string{T("pools.select_source")}, []string{""}
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
				options = append(options, SanitizeDisplayText(sub.Name))
				optionIDs = append(optionIDs, sub.ID)
			}
			selector.SetOptions(options, nil)
			addMember := tview.NewButton(T("pools.add_member_button")).SetSelectedFunc(func() {
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
				newOptions, newOptionIDs := []string{T("pools.select_source")}, []string{""}
				for _, sub := range cfg.Subscriptions {
					inPool := false
					for _, mid := range members {
						if mid == sub.ID {
							inPool = true
							break
						}
					}
					if !inPool {
						newOptions = append(newOptions, SanitizeDisplayText(sub.Name))
						newOptionIDs = append(newOptionIDs, sub.ID)
					}
				}
				options, optionIDs = newOptions, newOptionIDs
				selector.SetOptions(options, nil)
				selector.SetCurrentOption(0)
			})
			up := tview.NewButton(T("pools.move_up")).SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i > 0 && i < len(members) {
					members[i-1], members[i] = members[i], members[i-1]
					rebuildMembers()
					memberList.SetCurrentItem(i - 1)
				}
			})
			down := tview.NewButton(T("pools.move_down")).SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i >= 0 && i < len(members)-1 {
					members[i+1], members[i] = members[i], members[i+1]
					rebuildMembers()
					memberList.SetCurrentItem(i + 1)
				}
			})
			activate := tview.NewButton(T("pools.set_active")).SetSelectedFunc(func() {
				i := memberList.GetCurrentItem()
				if i >= 0 && i < len(members) {
					active = members[i]
					rebuildMembers()
				}
			})
			remove := tview.NewButton(T("pools.remove_member")).SetSelectedFunc(func() {
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
					newOptions, newOptionIDs := []string{T("pools.select_source")}, []string{""}
					for _, sub := range cfg.Subscriptions {
						inPool := false
						for _, mid := range members {
							if mid == sub.ID {
								inPool = true
								break
							}
						}
						if !inPool {
							newOptions = append(newOptions, SanitizeDisplayText(sub.Name))
							newOptionIDs = append(newOptionIDs, sub.ID)
						}
					}
					options, optionIDs = newOptions, newOptionIDs
					selector.SetOptions(options, nil)
					selector.SetCurrentOption(0)
				}
			})
			toggleDisable := tview.NewButton(T("pools.toggle_disabled")).SetSelectedFunc(func() {
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
							showModal(T("pools.toggle_failed"), err.Error())
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
			save := tview.NewButton(T("pools.save_button"))
			cancel := tview.NewButton(T("pools.cancel_button")).SetSelectedFunc(close)
			save.SetSelectedFunc(func() {
				parsed, err := strconv.Atoi(strings.TrimSpace(intervalField.GetText()))
				if err != nil || parsed <= 0 {
					showModal(T("pools.save_failed"), T("pools.invalid_refresh_interval"))
					return
				}
				if enabledBox.IsChecked() && len(members) == 0 {
					showModal(T("pools.save_failed"), T("pools.member_required"))
					return
				}
				req := poolRequest(members, active, strings.TrimSpace(nameField.GetText()), mode, enabledBox.IsChecked(), parsed)
				go func() {
					client, err := mihomotui.GetIPCClient()
					var report *mihomotui.SubscriptionPoolUpdateResponse
					if err == nil {
						if existing == nil {
							report, err = client.IPCCreateSubscriptionPool(req)
						} else {
							report, err = client.IPCUpdateSubscriptionPool(existing.ID, req)
						}
					}
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal(T("pools.save_failed"), err.Error())
							return
						}
						close()
						load(func(e error) {
							if e != nil {
								showModal(T("pools.sync_failed"), e.Error())
								return
							}
							render()
							if report != nil && !report.Applied {
								showModal(T("pools.saved_not_applied"), applyFailureText(report))
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
			empty := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetText(T("pools.empty"))
			list.AddItem(empty, 0, 1, false)
			return
		}
		for i := range cfg.SubscriptionPools {
			pool := cfg.SubscriptionPools[i]
			availability := cfg.PoolAvailability(pool)
			activeName, cache := T("pools.none"), T("pools.none")
			state := T("pools.state_disabled")
			if si := cfg.FindSubscriptionByID(pool.ActiveMemberID); si >= 0 {
				sub := cfg.Subscriptions[si]
				activeName = SanitizeDisplayText(sub.Name)
				if mihomotui.SubscriptionCacheAvailable(sub) {
					cache = T("pools.cache_available")
				} else {
					cache = T("pools.cache_missing")
				}
				state = T("pools.state_normal")
				if sub.FailureCount > 0 {
					state = Tf("pools.consecutive_failures", map[string]any{"Count": sub.FailureCount})
				}
			}
			if pool.Degraded {
				state = T("pools.state_degraded")
			}
			reason := strings.TrimSpace(pool.LastSwitchReason)
			if reason == "" {
				reason = T("pools.none")
			}
			modeText := T("pools.mode_failover")
			if pool.Mode == mihomotui.SubscriptionPoolModeMerge {
				modeText = T("pools.mode_merge")
				activeName = fmt.Sprintf(T("pools.merge_participation"), availability.Available, availability.Total)
				cache = fmt.Sprintf(T("pools.merge_cache"), availability.Available, availability.Total)
			}
			// 池被禁用时成员完全不参与运行，必须在卡片上直接说明，
			// 否则只会看到"保存成功"和内核里过期的旧节点。
			runHint := ""
			if !pool.Enabled {
				runHint = T("pools.disabled_run_hint")
			}
			skippedText := ""
			if len(availability.Skipped) > 0 {
				parts := make([]string, 0, len(availability.Skipped))
				for _, s := range availability.Skipped {
					name := SanitizeDisplayText(s.Name)
					if name == "" {
						name = SanitizeDisplayText(s.ID)
					}
					parts = append(parts, name+"("+s.Reason+")")
				}
				skippedText = Tf("pools.skipped", map[string]any{"Members": strings.Join(parts, T("pools.member_separator"))})
			}
			content := fmt.Sprintf(T("pools.card_info"),
				SanitizeDisplayText(pool.Name), state, modeText, activeName, cache, len(pool.Members), pool.RefreshInterval, pool.LastSwitchAt, reason, runHint, skippedText)
			info := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetText(content)
			p := pool
			edit := tview.NewButton(T("pools.edit_button")).SetSelectedFunc(func() { showEditor(&p) })
			flush := tview.NewButton(T("pools.refresh_button")).SetSelectedFunc(func() {
				go func() {
					c, e := mihomotui.GetIPCClient()
					if e == nil {
						e = c.IPCRefreshSubscriptionPool(p.ID)
					}
					app.QueueUpdateDraw(func() {
						if e != nil {
							showModal(T("pools.refresh_failed"), e.Error())
							return
						}
						load(func(err error) {
							if err != nil {
								showModal(T("pools.sync_failed"), err.Error())
							} else {
								render()
							}
						})
					})
				}()
			})
			deleteBtn := tview.NewButton(T("pools.delete_button")).SetSelectedFunc(func() {
				go func() {
					c, e := mihomotui.GetIPCClient()
					var report *mihomotui.SubscriptionPoolUpdateResponse
					if e == nil {
						report, e = c.IPCDeleteSubscriptionPool(p.ID)
					}
					app.QueueUpdateDraw(func() {
						if e != nil {
							showModal(T("pools.delete_failed"), e.Error())
							return
						}
						load(func(err error) {
							if err != nil {
								showModal(T("pools.sync_failed"), err.Error())
								return
							}
							render()
							if report != nil && !report.Applied {
								showModal(T("pools.deleted_not_applied"), applyFailureText(report))
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
				showModal(T("pools.refresh_failed"), err.Error())
				return
			}
			render()
		})
	})
	load(func(err error) {
		if err != nil {
			status.SetText(Tf("pools.load_failed", map[string]any{"Error": err.Error()}))
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
