package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// Subscription 订阅数据模型
type Subscription struct {
	ID                 string
	Name               string
	URL                string
	UpdatedAt          string
	LastSuccessAt      string
	LastFailureAt      string
	LastError          string
	UsedGB             float64
	TotalGB            float64
	UploadBytes        int64
	DownloadBytes      int64
	TotalBytes         int64
	RemainingBytes     int64
	ExpireAt           string
	MetadataAvailable  bool
	MetadataStatus     string
	FailureCount       int
	CacheFile          string
	CacheAvailable     bool
	Disabled           bool
	SourceType         mihomotui.SubscriptionSource
	UseLocalProxy      bool
	UserAgent          string
	FetchProxyStrategy mihomotui.SubscriptionFetchProxyStrategy
}

// NewSubscriptionPage 创建订阅页面
func NewSubscriptionPage(app *tview.Application) tview.Primitive {
	cfg := mihomotui.GlobalConfig()

	// 从 globalConfig 加载订阅数据
	subscriptions := []Subscription{}
	reloadSubs := func() {
		subscriptions = make([]Subscription, 0, len(cfg.Subscriptions))
		for _, meta := range cfg.Subscriptions {
			subscriptions = append(subscriptions, Subscription{
				ID:            meta.ID,
				Name:          meta.Name,
				URL:           meta.URL,
				UpdatedAt:     meta.UpdatedAt,
				LastSuccessAt: meta.LastSuccessAt,
				LastFailureAt: meta.LastFailureAt,
				LastError:     meta.LastError,
				UsedGB:        meta.UsedGB,
				TotalGB:       meta.TotalGB,
				UploadBytes:   meta.UploadBytes, DownloadBytes: meta.DownloadBytes, TotalBytes: meta.TotalBytes,
				ExpireAt: meta.ExpireAt, MetadataAvailable: meta.MetadataAvailable, MetadataStatus: meta.MetadataStatus,
				FailureCount: meta.FailureCount,
				CacheFile:    meta.CacheFile,
				// 缓存是否可用与首页、订阅池页共用同一判定，避免同一条订阅
				// 在不同页面显示不同结论。
				CacheAvailable: mihomotui.SubscriptionCacheAvailable(meta),
				Disabled:       meta.Disabled,
				SourceType:     meta.SourceType,
				UseLocalProxy:  meta.UseLocalProxy, UserAgent: meta.UserAgent, FetchProxyStrategy: meta.FetchProxyStrategy,
			})
		}
	}
	reloadSubs()

	// Pages 容器，用于支持弹窗覆盖
	pages := tview.NewPages()
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	// 弹窗显示
	showModal := func(title, message string) {
		modal := tview.NewModal().
			SetText(fmt.Sprintf("%s\n\n%s", title, message)).
			AddButtons([]string{T("common.confirm")}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				pages.HidePage("modal")
				pages.RemovePage("modal")
			})
		pages.AddPage("modal", modal, true, true)
	}

	currentPage := 0
	maxPerPage := 4 // 初始值，会根据终端高度自适应
	cardHeight := 8 // 每个卡片占用的行数（3行内容 + 3个按钮 + 2个间隔 + 边框）
	selectedSub := 0

	// 输入框
	inputField := tview.NewInputField().
		SetPlaceholder(T("subscription.url_placeholder")).
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	inputField.SetBorder(true)

	// 导入按钮
	importBtn := tview.NewButton(T("subscription.import_button"))
	importBtn.SetBorder(false)

	// 文件/粘贴导入按钮
	contentBtn := tview.NewButton(T("subscription.content_button"))
	contentBtn.SetBorder(false)

	// 新建按钮
	newBtn := tview.NewButton(T("subscription.new_button"))
	newBtn.SetBorder(false)

	// 顶部工具栏：按钮之间用 2 格空隙隔开，避免挤在一起。
	toolbar := tview.NewFlex().
		AddItem(inputField, 0, 1, false).
		AddItem(nil, 2, 0, false).
		AddItem(importBtn, 10, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(contentBtn, 14, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(newBtn, 10, 0, false)

	// 订阅卡片列表容器
	listFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	// 分页按钮
	prevBtn := tview.NewButton(" < ")
	prevBtn.SetBorder(false)
	nextBtn := tview.NewButton(" > ")
	nextBtn.SetBorder(false)
	pageInfo := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	// 底部状态栏
	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")

	bottomBar := tview.NewFlex().
		AddItem(prevBtn, 5, 0, false).
		AddItem(pageInfo, 12, 0, false).
		AddItem(nextBtn, 5, 0, false).
		AddItem(statusBar, 0, 1, false)

	// 计算总页数
	totalPages := func() int {
		if len(subscriptions) == 0 {
			return 1
		}
		return (len(subscriptions) + maxPerPage - 1) / maxPerPage
	}

	// 更新分页信息
	updatePager := func() {
		tp := totalPages()
		pageInfo.SetText(fmt.Sprintf(" %d / %d ", currentPage+1, tp))
	}

	// 先声明闭包变量，避免循环引用
	refreshSub := func(idx int) {}
	deleteSub := func(idx int) {}
	editSub := func(idx int) {}
	refreshCards := func() {}

	// 刷新指定订阅：由 daemon 实际请求远端订阅、更新元数据，并在激活时应用新配置。
	refreshSub = func(idx int) {
		if idx < 0 || idx >= len(subscriptions) {
			return
		}
		sub := subscriptions[idx]
		name := sub.Name
		identifier := sub.ID
		if identifier == "" {
			identifier = name // 兼容未迁移稳定 ID 的历史配置。
		}
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { showModal(T("subscription.refresh_failed"), err.Error()) })
				return
			}
			if err := client.IPCRefreshSubscription(identifier); err != nil {
				app.QueueUpdateDraw(func() { showModal(T("subscription.refresh_failed"), err.Error()) })
				return
			}
			cfg2, err := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if err != nil {
					showModal(T("subscription.refresh_sync_failed"), err.Error())
					return
				}
				cfg = cfg2
				mihomotui.SyncConfigFromServer(cfg2)
				mihomotui.ResetMihomoAPI()
				reloadSubs()
				refreshCards()
				showModal(T("subscription.refresh_success"), Tf("subscription.refreshed_name", map[string]any{"Name": name}))
			})
		}()
	}

	// 删除指定订阅（通过 IPC）
	deleteSub = func(idx int) {
		if idx < 0 || idx >= len(subscriptions) {
			return
		}
		sub := subscriptions[idx]
		name := sub.Name
		identifier := sub.ID
		if identifier == "" {
			identifier = name // 兼容未迁移稳定 ID 的历史配置。
		}
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("subscription.delete_failed"), err.Error())
				})
				return
			}
			if err := client.IPCDeleteSubscription(identifier); err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("subscription.delete_failed"), err.Error())
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
				reloadSubs()
				if selectedSub >= len(subscriptions) && len(subscriptions) > 0 {
					selectedSub = len(subscriptions) - 1
				}
				refreshCards()
			})
		}()
	}

	// editSub 修改订阅显示名称；远程订阅还可修改 URL 与本地代理拉取开关。
	editSub = func(idx int) {
		if idx < 0 || idx >= len(subscriptions) {
			return
		}
		sub := subscriptions[idx]
		var nameField *tview.InputField
		ShowOverlay(app, pages, "subscription-editor", T("subscription.editor_title"), func(close func()) (tview.Primitive, tview.Primitive) {
			form := tview.NewForm().SetButtonsAlign(tview.AlignRight)
			nameField = tview.NewInputField().SetLabel(T("subscription.name_label")).SetText(sub.Name)
			urlField := tview.NewInputField().SetLabel(T("subscription.url_label")).SetText(sub.URL)
			strategyLabels := []string{T("subscription.fetch_direct"), T("subscription.fetch_local_mihomo"), T("subscription.fetch_system")}
			strategyValues := []mihomotui.SubscriptionFetchProxyStrategy{mihomotui.SubscriptionFetchDirect, mihomotui.SubscriptionFetchLocalMihomo, mihomotui.SubscriptionFetchSystem}
			selectedStrategy := 0
			for i, value := range strategyValues {
				if sub.FetchProxyStrategy == value || (sub.FetchProxyStrategy == "" && sub.UseLocalProxy && value == mihomotui.SubscriptionFetchLocalMihomo) {
					selectedStrategy = i
				}
			}
			strategyField := tview.NewDropDown().SetLabel(T("subscription.fetch_network_label")).SetOptions(strategyLabels, nil).SetCurrentOption(selectedStrategy)
			userAgentField := tview.NewInputField().SetLabel(T("subscription.user_agent_label")).SetText(sub.UserAgent)
			// 对文件/粘贴导入，正文不可从 IPC 读回；只允许改名称，避免误以为能编辑缓存正文。
			isRemote := sub.SourceType == "" || sub.SourceType == mihomotui.SubscriptionSourceURL
			if !isRemote {
				urlField.SetDisabled(true)
				strategyField.SetDisabled(true)
				userAgentField.SetDisabled(true)
			}
			form.AddFormItem(nameField).AddFormItem(urlField).AddFormItem(strategyField).AddFormItem(userAgentField)
			save := tview.NewButton(T("subscription.save_button"))
			cancel := tview.NewButton(T("subscription.cancel_button")).SetSelectedFunc(close)
			save.SetSelectedFunc(func() {
				name, rawURL := strings.TrimSpace(nameField.GetText()), strings.TrimSpace(urlField.GetText())
				if name == "" {
					showModal(T("subscription.save_failed"), T("subscription.name_required"))
					return
				}
				if isRemote && (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
					showModal(T("subscription.save_failed"), T("subscription.remote_url_invalid"))
					return
				}
				strategyIndex, _ := strategyField.GetCurrentOption()
				strategy := strategyValues[strategyIndex]
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err == nil {
						err = client.IPCUpdateSubscription(sub.ID, mihomotui.SubscriptionUpdateRequest{Name: name, URL: rawURL, UseLocalProxy: strategy == mihomotui.SubscriptionFetchLocalMihomo, FetchProxyStrategy: strategy, UserAgent: strings.TrimSpace(userAgentField.GetText())})
					}
					if err != nil {
						app.QueueUpdateDraw(func() { showModal(T("subscription.save_failed"), err.Error()); app.SetFocus(nameField) })
						return
					}
					fresh, err := client.IPCGetConfig()
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal(T("subscription.save_sync_failed"), err.Error())
							return
						}
						cfg = fresh
						mihomotui.SyncConfigFromServer(fresh)
						mihomotui.ResetMihomoAPI()
						reloadSubs()
						refreshCards()
						close()
						showModal(T("subscription.save_success"), T("subscription.save_success_message"))
					})
				}()
			})
			actions := tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(save, 12, 0, true).AddItem(cancel, 12, 0, false)
			body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 6, 0, true).AddItem(tview.NewTextView().SetText(func() string {
				if isRemote {
					return T("subscription.editor_remote_hint")
				}
				return T("subscription.editor_local_hint")
			}()), 2, 0, false).AddItem(actions, 1, 0, false)
			return body, nameField
		})
	}

	// 刷新卡片列表
	refreshCards = func() {
		listFlex.Clear()

		// 页码边界检查
		tp := totalPages()
		if currentPage >= tp {
			currentPage = tp - 1
		}
		if currentPage < 0 {
			currentPage = 0
		}

		start := currentPage * maxPerPage
		end := min(start+maxPerPage, len(subscriptions))

		for i := start; i < end; i++ {
			idx := i
			sub := &subscriptions[idx]

			// 计算进度条
			percent := 0.0
			percentText := "—"
			if sub.TotalGB > 0 {
				percent = sub.UsedGB / sub.TotalGB * 100
				percentText = fmt.Sprintf("%.0f%%", percent)
			}
			progressWidth := 20
			filled := min(int(percent/100*float64(progressWidth)), progressWidth)
			bar := strings.Repeat("━", filled) + strings.Repeat("─", progressWidth-filled)

			// 信息文本（3行）
			statusText := T("subscription.cache_active")
			switch {
			case sub.CacheFile == "":
				statusText = T("subscription.cache_pending")
			case !sub.CacheAvailable && !sub.Disabled:
				statusText = T("subscription.cache_missing")
			}
			if sub.FailureCount > 0 {
				statusText = Tf("subscription.consecutive_failures", map[string]any{"Count": sub.FailureCount})
			}
			if sub.LastError != "" {
				statusText = Tf("subscription.last_refresh_error", map[string]any{"Error": mihomotui.RedactURLInText(sub.LastError)})
			}
			quotaText := T("subscription.metadata_unavailable")
			if sub.MetadataAvailable {
				if sub.RemainingBytes > 0 && sub.TotalBytes == 0 {
					quotaText = fmt.Sprintf(T("subscription.quota_remaining"), float64(sub.RemainingBytes)/(1024*1024*1024))
				} else {
					quotaText = fmt.Sprintf(T("subscription.quota_usage"), sub.UsedGB, sub.TotalGB)
				}
				if sub.ExpireAt != "" {
					quotaText += Tf("subscription.quota_expiry", map[string]any{"Date": sub.ExpireAt})
				}
			}
			// 计算该订阅已分配到哪些订阅池；未归入任何池即"未分配"，不参与运行。
			poolNames := []string{}
			for _, pool := range cfg.SubscriptionPools {
				for _, memberID := range pool.Members {
					if memberID == sub.ID {
						label := SanitizeDisplayText(pool.Name)
						if pool.Mode == mihomotui.SubscriptionPoolModeMerge {
							label += T("subscription.pool_merge_suffix")
						}
						if !pool.Enabled {
							label += T("subscription.pool_disabled_suffix")
						}
						poolNames = append(poolNames, label)
						break
					}
				}
			}
			poolText := T("subscription.pool_unassigned")
			if len(poolNames) > 0 {
				poolText = strings.Join(poolNames, ", ")
			}
			infoText := fmt.Sprintf(
				T("subscription.card_info"),
				// 订阅名由用户输入，可能含国旗 emoji 或方括号标签语法。
				SanitizeDisplayText(sub.Name), quotaText, mihomotui.RedactURL(sub.URL), sub.UpdatedAt, percentText, mihomotui.ColorInfo, bar, statusText, poolText,
			)
			info := tview.NewTextView().SetText(infoText).SetDynamicColors(true)

			// 编辑按钮
			editBtn := tview.NewButton(T("subscription.edit_button"))
			editBtn.SetBorder(false)
			editBtn.SetSelectedFunc(func() { editSub(idx) })

			// 刷新按钮
			refreshBtn := tview.NewButton(T("subscription.refresh_button"))
			refreshBtn.SetBorder(false)
			refreshBtn.SetSelectedFunc(func() {
				refreshSub(idx)
			})

			// 删除按钮
			deleteBtn := tview.NewButton(T("subscription.delete_button"))
			deleteBtn.SetBorder(false)
			deleteBtn.SetSelectedFunc(func() {
				deleteSub(idx)
			})

			// 按钮区：编辑 + 刷新 + 删除（按钮之间留 1 行空隙）
			btnFlex := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(editBtn, 1, 0, true).
				AddItem(nil, 1, 0, false).
				AddItem(refreshBtn, 1, 0, true).
				AddItem(nil, 1, 0, false).
				AddItem(deleteBtn, 1, 0, true)

			// 卡片布局：左侧信息 + 右侧按钮区
			card := tview.NewFlex().
				AddItem(info, 0, 1, false).
				AddItem(btnFlex, 7, 0, true)
			card.SetBorder(true)

			// 选中高亮
			if idx == selectedSub {
				card.SetBorderColor(tcell.ColorBlue)
				card.SetBorderAttributes(tcell.AttrBold)
			}

			listFlex.AddItem(card, cardHeight, 0, false)
		}

		// 空列表提示
		if len(subscriptions) == 0 {
			empty := tview.NewTextView().
				SetTextAlign(tview.AlignCenter).
				SetText(T("subscription.empty"))
			listFlex.AddItem(empty, 0, 1, false)
		}

		updatePager()
	}

	// 上一页
	prevBtn.SetSelectedFunc(func() {
		if currentPage > 0 {
			currentPage--
			refreshCards()
		}
	})

	// 下一页
	nextBtn.SetSelectedFunc(func() {
		if currentPage < totalPages()-1 {
			currentPage++
			refreshCards()
		}
	})

	// 导入按钮回调（通过 IPC）
	importBtn.SetSelectedFunc(func() {
		url := strings.TrimSpace(inputField.GetText())
		if url == "" {
			return
		}

		// URL 格式校验
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			showModal(T("subscription.import_failed"), T("subscription.import_url_invalid"))
			return
		}

		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("subscription.import_failed"), err.Error())
				})
				return
			}
			if err := client.IPCImportSubscription(url); err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("subscription.import_failed"), err.Error())
				})
				return
			}
			// 同步最新配置
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					cfg = cfg2
					mihomotui.SyncConfigFromServer(cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadSubs()
				inputField.SetText("")
				currentPage = totalPages() - 1
				refreshCards()
				if len(subscriptions) > 0 {
					showModal(T("subscription.import_success"), Tf("subscription.import_success_name", map[string]any{"Name": subscriptions[len(subscriptions)-1].Name}))
				} else {
					showModal(T("subscription.import_success"), T("subscription.import_success_generic"))
				}
			})
		}()
	})

	// 文件/粘贴导入：使用显式来源模式并将焦点固定到弹窗，避免粘贴落入背景 URL 输入框。
	contentBtn.SetSelectedFunc(func() {
		ShowOverlay(app, pages, "content-import", T("subscription.content_import_title"), func(close func()) (tview.Primitive, tview.Primitive) {
			form := tview.NewForm()
			nameField := tview.NewInputField().SetLabel(T("subscription.name_label"))
			modeField := tview.NewDropDown().SetLabel(T("subscription.source_label")).SetOptions([]string{T("subscription.source_pasted"), T("subscription.source_file")}, nil)
			contentField := tview.NewTextArea().SetPlaceholder(T("subscription.content_placeholder"))
			pathField := tview.NewInputField().SetLabel(T("subscription.file_path_label")).SetPlaceholder("/path/to/subscription.yaml")
			pathField.SetDisabled(true)
			modeField.SetSelectedFunc(func(_ string, index int) {
				isFile := index == 1
				contentField.SetDisabled(isFile)
				pathField.SetDisabled(!isFile)
				if isFile {
					form.SetFocus(2)
					app.SetFocus(pathField)
				} else {
					form.SetFocus(1)
					app.SetFocus(contentField)
				}
			})
			form.AddFormItem(nameField).AddFormItem(contentField).AddFormItem(pathField).AddFormItem(modeField)
			submitting := false
			form.AddButton(T("subscription.import"), func() {
				if submitting {
					return
				}
				name := strings.TrimSpace(nameField.GetText())
				selected, _ := modeField.GetCurrentOption()
				content := strings.TrimSpace(contentField.GetText())
				source, sourceType := "Pasted content", mihomotui.SubscriptionSourceContent
				if selected == 1 {
					path := strings.TrimSpace(pathField.GetText())
					if path == "" {
						showModal(T("subscription.import_failed"), T("subscription.local_file_required"))
						return
					}
					data, err := os.ReadFile(path)
					if err != nil {
						showModal(T("subscription.read_file_failed"), err.Error())
						return
					}
					content, source, sourceType = string(data), path, mihomotui.SubscriptionSourceFile
				}
				if content == "" {
					showModal(T("subscription.import_failed"), T("subscription.content_required"))
					return
				}
				submitting = true
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err == nil {
						err = client.IPCImportSubscriptionContent(name, source, sourceType, content, false)
					}
					if err != nil {
						app.QueueUpdateDraw(func() {
							submitting = false
							showModal(T("subscription.import_failed"), err.Error())
							app.SetFocus(contentField)
						})
						return
					}
					cfg2, syncErr := client.IPCGetConfig()
					app.QueueUpdateDraw(func() {
						submitting = false
						if syncErr != nil {
							showModal(T("subscription.import_sync_failed"), syncErr.Error())
							app.SetFocus(contentField)
							return
						}
						cfg = cfg2
						mihomotui.SyncConfigFromServer(cfg2)
						mihomotui.ResetMihomoAPI()
						reloadSubs()
						refreshCards()
						close()
						showModal(T("subscription.import_success"), T("subscription.content_import_success"))
					})
				}()
			})
			form.AddButton(T("common.cancel"), close)
			return form, contentField
		})
	})

	// 新建按钮回调（通过 IPC）
	newBtn.SetSelectedFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal(T("subscription.create_failed"), err.Error())
				})
				return
			}
			if err := client.IPCImportSubscriptionWithRequest(mihomotui.SubscriptionImportRequest{Name: T("subscription.new_default_name"), Manual: true}); err != nil {
				app.QueueUpdateDraw(func() { showModal(T("subscription.create_failed"), err.Error()) })
				return
			}
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					cfg = cfg2
					mihomotui.SyncConfigFromServer(cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadSubs()
				currentPage = totalPages() - 1
				refreshCards()
			})
		}()
	})

	// 主布局：toolbar 与列表、列表与底部栏之间各留 1 行空隙。
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(listFlex, 0, 1, false).
		AddItem(nil, 1, 0, false).
		AddItem(bottomBar, 1, 0, false)

	// 页面级键盘捕获：方向键在订阅卡片间导航
	page.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() != page {
			return event
		}

		total := len(subscriptions)

		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(inputField)
			return nil
		case tcell.KeyDown:
			if selectedSub < total-1 {
				selectedSub++
				if selectedSub >= (currentPage+1)*maxPerPage {
					currentPage++
				}
				refreshCards()
			}
			return nil
		case tcell.KeyUp:
			if selectedSub > 0 {
				selectedSub--
				if selectedSub < currentPage*maxPerPage {
					currentPage--
				}
				refreshCards()
			}
			return nil
		case tcell.KeyEnter:
			// Enter 触发刷新当前选中的订阅
			if selectedSub >= 0 && selectedSub < total {
				refreshSub(selectedSub)
			}
			return nil
		}
		return event
	})

	// 自适应：根据终端高度动态计算每页可显示的卡片数量
	lastHeight := 0
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// toolbar 占 3 行，bottomBar 占 1 行，listFlex 可用高度 = height - 4
		available := max(height-4, cardHeight)
		perPage := max(available/cardHeight, 1)

		// 只在高度或每页数量变化时重新刷新，避免无限循环
		if height != lastHeight || perPage != maxPerPage {
			lastHeight = height
			maxPerPage = perPage
			refreshCards()
		}

		// 在底部状态栏显示自适应信息
		statusBar.SetText(fmt.Sprintf(T("subscription.page_status"), len(subscriptions), maxPerPage))

		return x, y, width, height
	})

	refreshCards()

	// 每次进入订阅页都从 daemon 回读，确保后台从运行中 provider 同步的额度
	// 不会一直停留在 TUI 启动时的旧快照。
	pages.SetFocusFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				return
			}
			latest, err := client.IPCGetConfig()
			if err != nil || latest == nil {
				return
			}
			app.QueueUpdateDraw(func() {
				cfg = latest
				mihomotui.SyncConfigFromServer(latest)
				reloadSubs()
				refreshCards()
			})
		}()
	})

	pages.AddPage("main", page, true, true)
	return pages
}
