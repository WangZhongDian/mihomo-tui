package ui

// TODO: 当前为模拟数据页面，所有解锁测试结果由 math/rand 随机生成。
// 后续需要接入真实的流媒体解锁检测接口（如通过代理访问各服务 API）。

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// TestItem 解锁测试项
type TestItem struct {
	Name    string
	Status  string // 支持, 不支持, 测试失败, 待检测
	Region  string // SG, SGP, US 等
	Updated string
}

// NewTestPage 创建测试页面
func NewTestPage(app *tview.Application) tview.Primitive {
	items := []TestItem{
		{Name: "哔哩哔哩大陆", Status: "支持", Region: "", Updated: "2025-08-21 11:46:18"},
		{Name: "哔哩哔哩港澳台", Status: "不支持", Region: "", Updated: "2025-08-21 11:46:18"},
		{Name: "Bahamut Anime", Status: "测试失败", Region: "", Updated: "2025-08-21 11:46:19"},
		{Name: "ChatGPT iOS", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:19"},
		{Name: "ChatGPT Web", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:19"},
		{Name: "Claude", Status: "待检测", Region: "", Updated: "--"},
		{Name: "Disney+", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:21"},
		{Name: "Gemini", Status: "支持", Region: "SGP", Updated: "2025-08-21 11:46:20"},
		{Name: "Netflix", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:19"},
		{Name: "Prime Video", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:20"},
		{Name: "YouTube", Status: "支持", Region: "", Updated: "2025-08-21 11:46:22"},
		{Name: "TikTok", Status: "不支持", Region: "", Updated: "2025-08-21 11:46:23"},
		{Name: "Spotify", Status: "支持", Region: "SG", Updated: "2025-08-21 11:46:21"},
		{Name: "HBO Max", Status: "待检测", Region: "", Updated: "--"},
		{Name: "Hulu", Status: "支持", Region: "US", Updated: "2025-08-21 11:46:20"},
	}

	currentPage := 0
	maxPerPage := 4
	cardsPerRow := 2
	cardHeight := 5
	rowGap := 1 // 行与行之间的间距
	selectedTest := 0

	// 顶部工具栏
	testAllBtn := tview.NewButton(" 测试全部 ")
	testAllBtn.SetBorder(false)

	toolbar := tview.NewFlex().
		AddItem(tview.NewTextView().SetText(" 解锁测试"), 0, 1, false).
		AddItem(testAllBtn, 14, 0, false)

	// 卡片网格
	cardsFlex := tview.NewFlex().SetDirection(tview.FlexRow)

	// 分页按钮
	prevBtn := tview.NewButton(" < ")
	prevBtn.SetBorder(false)
	nextBtn := tview.NewButton(" > ")
	nextBtn.SetBorder(false)
	pageInfo := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	bottomBar := tview.NewFlex().
		AddItem(prevBtn, 5, 0, false).
		AddItem(pageInfo, 12, 0, false).
		AddItem(nextBtn, 5, 0, false)

	// 随机测试单个 item
	testItem := func(item *TestItem) {
		statuses := []string{"支持", "不支持", "测试失败", "待检测"}
		regions := []string{"", "SG", "SGP", "US", "JP", "HK", "TW", "KR"}
		item.Status = statuses[rand.Intn(len(statuses))]
		if item.Status == "支持" {
			item.Region = regions[rand.Intn(len(regions))]
		} else {
			item.Region = ""
		}
		if item.Status == "待检测" {
			item.Updated = "--"
		} else {
			item.Updated = time.Now().Format("2006-01-02 15:04:05")
		}
	}

	// 计算总页数
	totalPages := func() int {
		n := len(items)
		if n == 0 {
			return 1
		}
		return (n + maxPerPage - 1) / maxPerPage
	}

	// 刷新卡片
	refreshCards := func() {}
	refreshCards = func() {
		cardsFlex.Clear()

		tp := totalPages()
		if currentPage >= tp {
			currentPage = tp - 1
		}
		if currentPage < 0 {
			currentPage = 0
		}

		start := currentPage * maxPerPage
		end := min(start+maxPerPage, len(items))

		pageInfo.SetText(fmt.Sprintf(" %d / %d ", currentPage+1, tp))

		var rowFlex *tview.Flex
		count := 0
		for i := start; i < end; i++ {
			if count%cardsPerRow == 0 {
				if rowFlex != nil {
					cardsFlex.AddItem(rowFlex, cardHeight, 0, false)
					// 行间间距
					if i < end {
						cardsFlex.AddItem(tview.NewBox().SetBorder(false), rowGap, 0, false)
					}
				}
				rowFlex = tview.NewFlex()
			} else if count > 0 {
				// 卡片间水平间距
				rowFlex.AddItem(tview.NewBox().SetBorder(false), 1, 0, false)
			}

			idx := i
			card := createTestCard(&items[idx], idx == selectedTest, func() {
				testItem(&items[idx])
				refreshCards()
			})
			rowFlex.AddItem(card, 0, 1, false)
			count++
		}
		if rowFlex != nil {
			cardsFlex.AddItem(rowFlex, cardHeight, 0, false)
		}
	}

	// 测试全部
	testAllBtn.SetSelectedFunc(func() {
		for i := range items {
			testItem(&items[i])
		}
		refreshCards()
	})

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

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(cardsFlex, 0, 1, false).
		AddItem(bottomBar, 1, 0, false)

	// 页面级键盘捕获：方向键在测试卡片间导航
	page.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() != page {
			return event
		}

		total := len(items)

		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(testAllBtn)
			return nil
		case tcell.KeyRight:
			if selectedTest < total-1 {
				selectedTest++
				if selectedTest >= (currentPage+1)*maxPerPage {
					currentPage++
				}
				refreshCards()
			}
			return nil
		case tcell.KeyLeft:
			if selectedTest > 0 {
				selectedTest--
				if selectedTest < currentPage*maxPerPage {
					currentPage--
				}
				refreshCards()
			}
			return nil
		case tcell.KeyDown:
			if selectedTest+cardsPerRow < total {
				selectedTest += cardsPerRow
				if selectedTest >= (currentPage+1)*maxPerPage {
					currentPage = selectedTest / maxPerPage
				}
				refreshCards()
			}
			return nil
		case tcell.KeyUp:
			if selectedTest-cardsPerRow >= 0 {
				selectedTest -= cardsPerRow
				if selectedTest < currentPage*maxPerPage {
					currentPage = selectedTest / maxPerPage
				}
				refreshCards()
			}
			return nil
		case tcell.KeyEnter:
			// Enter 触发当前选中项的测试
			if selectedTest >= 0 && selectedTest < total {
				testItem(&items[selectedTest])
				refreshCards()
			}
			return nil
		}
		return event
	})

	// 自适应：根据终端宽高动态计算
	lastWidth, lastHeight := 0, 0
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// 卡片宽度 + 间距
		newCardsPerRow := min(max(width/26, 1), 4)
		availableHeight := max(
			// toolbar(3) + bottomBar(1)
			height-4, cardHeight+rowGap)
		rowsPerPage := max(availableHeight/(cardHeight+rowGap), 1)

		newMaxPerPage := rowsPerPage * newCardsPerRow

		if width != lastWidth || height != lastHeight ||
			newMaxPerPage != maxPerPage || newCardsPerRow != cardsPerRow {
			lastWidth = width
			lastHeight = height
			maxPerPage = newMaxPerPage
			cardsPerRow = newCardsPerRow
			refreshCards()
		}

		return x, y, width, height
	})

	refreshCards()

	return page
}

// createTestCard 创建测试卡片
func createTestCard(item *TestItem, selected bool, onTest func()) tview.Primitive {
	// 状态颜色
	statusColor := "gray"
	switch item.Status {
	case "支持":
		statusColor = "green"
	case "不支持", "测试失败":
		statusColor = "red"
	}

	// 地区标签
	regionTag := ""
	if item.Region != "" {
		regionTag = fmt.Sprintf(" [white:blue] %s [-:-:-]", item.Region)
	}

	// 卡片文本（3行）
	text := fmt.Sprintf(
		" [::b]%s[-:-:-]\n"+
			" [%s]● %s[-]%s\n"+
			" [%s]%s[-]",
		mihomotui.ColorMuted,
		item.Name,
		statusColor, item.Status, regionTag,
		item.Updated,
	)

	info := tview.NewTextView().
		SetText(text).
		SetDynamicColors(true)
	info.SetBorder(true)

	if selected {
		info.SetBorderColor(tcell.ColorBlue)
		info.SetBorderAttributes(tcell.AttrBold)
	}

	// 测试按钮
	testBtn := tview.NewButton(" ↻ ")
	testBtn.SetBorder(false)
	testBtn.SetSelectedFunc(onTest)

	// 间距
	spacer := tview.NewBox().SetBorder(false)

	// 卡片布局：左侧信息 + 间距 + 右侧按钮
	layout := tview.NewFlex().
		AddItem(info, 0, 1, false).
		AddItem(spacer, 1, 0, false).
		AddItem(testBtn, 3, 0, true)

	return layout
}
