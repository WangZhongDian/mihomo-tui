package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// Subscription 订阅数据模型
type Subscription struct {
	Name      string
	URL       string
	UpdatedAt string
	UsedGB    float64
	TotalGB   float64
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
				Name:      meta.Name,
				URL:       meta.URL,
				UpdatedAt: meta.UpdatedAt,
				UsedGB:    meta.UsedGB,
				TotalGB:   meta.TotalGB,
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
			AddButtons([]string{"确认"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				pages.HidePage("modal")
				pages.RemovePage("modal")
			})
		pages.AddPage("modal", modal, true, true)
	}

	currentPage := 0
	maxPerPage := 4 // 初始值，会根据终端高度自适应
	cardHeight := 6 // 每个卡片占用的行数（3行内容 + 3个按钮 + 边框）
	selectedSub := 0

	// 输入框
	inputField := tview.NewInputField().
		SetPlaceholder(" 订阅链接 (https://...)").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	inputField.SetBorder(true)

	// 导入按钮
	importBtn := tview.NewButton(" 导入 ")
	importBtn.SetBorder(false)

	// 新建按钮
	newBtn := tview.NewButton(" 新建 ")
	newBtn.SetBorder(false)

	// 顶部工具栏
	toolbar := tview.NewFlex().
		AddItem(inputField, 0, 1, false).
		AddItem(importBtn, 10, 0, false).
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
	refreshCards := func() {}

	// 刷新指定订阅
	refreshSub = func(idx int) {
		if idx < 0 || idx >= len(subscriptions) {
			return
		}
		sub := &subscriptions[idx]
		sub.UpdatedAt = time.Now().Format("2006-01-02 15:04")
		if sub.TotalGB > 0 {
			sub.UsedGB += 0.01
			if sub.UsedGB > sub.TotalGB {
				sub.UsedGB = sub.TotalGB
			}
		}
		// 同步回 globalConfig
		si := cfg.FindSubscriptionByName(sub.Name)
		if si >= 0 {
			cfg.Subscriptions[si].UpdatedAt = sub.UpdatedAt
			cfg.Subscriptions[si].UsedGB = sub.UsedGB
			cfg.Subscriptions[si].TotalGB = sub.TotalGB
			_ = cfg.Flush()
		}
		refreshCards()
	}

	// 删除指定订阅（通过 IPC）
	deleteSub = func(idx int) {
		if idx < 0 || idx >= len(subscriptions) {
			return
		}
		name := subscriptions[idx].Name
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("删除失败", err.Error())
				})
				return
			}
			if err := client.IPCDeleteSubscription(name); err != nil {
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
				reloadSubs()
				if selectedSub >= len(subscriptions) && len(subscriptions) > 0 {
					selectedSub = len(subscriptions) - 1
				}
				refreshCards()
			})
		}()
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
			if sub.TotalGB > 0 {
				percent = sub.UsedGB / sub.TotalGB * 100
			}
			progressWidth := 20
			filled := min(int(percent/100*float64(progressWidth)), progressWidth)
			bar := strings.Repeat("━", filled) + strings.Repeat("─", progressWidth-filled)

			// 信息文本（3行）
			infoText := fmt.Sprintf(
				"[blue::b] %s[-:-:-]    %.2fGB / %.2fGB\n"+
					" 来源: %s    更新: %s    %.0f%%\n"+
					" [blue]%s[-]",
				sub.Name, sub.UsedGB, sub.TotalGB, sub.URL, sub.UpdatedAt, percent, bar,
			)
			info := tview.NewTextView().SetText(infoText).SetDynamicColors(true)

			// 应用按钮（通过 IPC）
			applyBtn := tview.NewButton(" ✓ ")
			applyBtn.SetBorder(false)
			applyBtn.SetSelectedFunc(func() {
				subName := subscriptions[idx].Name
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err != nil {
						app.QueueUpdateDraw(func() {
							showModal("应用失败", err.Error())
						})
						return
					}
					if err := client.IPCApplySubscription(subName); err != nil {
						app.QueueUpdateDraw(func() {
							showModal("应用失败", err.Error())
						})
						return
					}
					// 同步最新配置并重置 API 客户端（secret 可能已变更）
					cfg2, _ := client.IPCGetConfig()
					if cfg2 != nil {
						mihomotui.SetGlobalConfig(*cfg2)
						mihomotui.ResetMihomoAPI()
					}
					app.QueueUpdateDraw(func() {
						showModal("应用成功", fmt.Sprintf("已应用订阅: %s\nmihomo 配置已生成", subName))
					})
				}()
			})

			// 刷新按钮
			refreshBtn := tview.NewButton(" ↻ ")
			refreshBtn.SetBorder(false)
			refreshBtn.SetSelectedFunc(func() {
				refreshSub(idx)
			})

			// 删除按钮
			deleteBtn := tview.NewButton(" ✕ ")
			deleteBtn.SetBorder(false)
			deleteBtn.SetSelectedFunc(func() {
				deleteSub(idx)
			})

			// 按钮区：应用 + 刷新 + 删除，垂直排列
			btnFlex := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(applyBtn, 1, 0, true).
				AddItem(refreshBtn, 1, 0, true).
				AddItem(deleteBtn, 1, 0, true)

			// 卡片布局：左侧信息 + 右侧按钮区
			card := tview.NewFlex().
				AddItem(info, 0, 1, false).
				AddItem(btnFlex, 5, 0, true)
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
				SetText("\n暂无订阅，请导入或新建")
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
			showModal("导入失败", "请输入以 http:// 或 https:// 开头的订阅链接")
			return
		}

		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("导入失败", err.Error())
				})
				return
			}
			if err := client.IPCImportSubscription(url); err != nil {
				app.QueueUpdateDraw(func() {
					showModal("导入失败", err.Error())
				})
				return
			}
			// 同步最新配置
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					mihomotui.SetGlobalConfig(*cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadSubs()
				inputField.SetText("")
				currentPage = totalPages() - 1
				refreshCards()
				showModal("导入成功", fmt.Sprintf("成功导入订阅: %s", subscriptions[len(subscriptions)-1].Name))
			})
		}()
	})

	// 新建按钮回调（通过 IPC）
	newBtn.SetSelectedFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					showModal("新建失败", err.Error())
				})
				return
			}
			if err := client.IPCImportSubscription("手动配置"); err != nil {
				// 手动配置的导入会失败，这里我们直接通过配置修改
				// 回退到本地操作
				_ = cfg.AddSubscription("新建订阅", "手动配置")
				_ = client.IPCUpdateConfig(cfg)
			}
			cfg2, _ := client.IPCGetConfig()
			app.QueueUpdateDraw(func() {
				if cfg2 != nil {
					mihomotui.SetGlobalConfig(*cfg2)
					mihomotui.ResetMihomoAPI()
				}
				reloadSubs()
				currentPage = totalPages() - 1
				refreshCards()
			})
		}()
	})

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(listFlex, 0, 1, false).
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
		statusBar.SetText(fmt.Sprintf(" 共%d条 每页%d条 ", len(subscriptions), maxPerPage))

		return x, y, width, height
	})

	refreshCards()

	pages.AddPage("main", page, true, true)
	return pages
}
