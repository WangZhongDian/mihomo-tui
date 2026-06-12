package ui

import (
	"fmt"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// NewProxyPage 创建代理页面
func NewProxyPage(app *tview.Application) tview.Primitive {
	var proxyGroups []mihomotui.ProxyGroup
	selectedGroup := 0
	selectedNode := 0
	currentPage := 0

	// 自适应参数
	maxPerPage := 4
	cardsPerRow := 2
	cardHeight := 4
	cardMinWidth := 24

	// 顶部工具栏：代理组下拉框 + 测试延迟按钮
	groupDropdown := tview.NewDropDown().
		SetLabel(" 代理组: ").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	testBtn := tview.NewButton(" 测试延迟 ")
	testBtn.SetBorder(false)

	toolbar := tview.NewFlex().
		AddItem(groupDropdown, 0, 1, false).
		AddItem(testBtn, 14, 0, false)

	// 节点卡片区域
	nodesFlex := tview.NewFlex().SetDirection(tview.FlexRow)

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

	// 计算总页数
	totalPages := func() int {
		if selectedGroup >= len(proxyGroups) {
			return 1
		}
		n := len(proxyGroups[selectedGroup].Nodes)
		if n == 0 {
			return 1
		}
		return (n + maxPerPage - 1) / maxPerPage
	}

	// 渲染节点卡片
	refreshNodes := func() {}
	refreshNodes = func() {
		nodesFlex.Clear()
		if selectedGroup >= len(proxyGroups) {
			empty := tview.NewTextView().
				SetTextAlign(tview.AlignCenter).
				SetText("\n暂无节点")
			nodesFlex.AddItem(empty, 0, 1, false)
			return
		}
		group := proxyGroups[selectedGroup]
		nodes := group.Nodes
		total := len(nodes)

		// 页码边界检查
		tp := totalPages()
		if currentPage >= tp {
			currentPage = tp - 1
		}
		if currentPage < 0 {
			currentPage = 0
		}

		start := currentPage * maxPerPage
		end := min(start+maxPerPage, total)

		pageInfo.SetText(fmt.Sprintf(" %d / %d ", currentPage+1, tp))

		var rowFlex *tview.Flex
		count := 0
		for i := start; i < end; i++ {
			if count%cardsPerRow == 0 {
				if rowFlex != nil {
					nodesFlex.AddItem(rowFlex, cardHeight, 0, false)
				}
				rowFlex = tview.NewFlex()
			}

			idx := i
			node := nodes[idx]
			card := createProxyCard(node, idx == selectedNode, func() {
				go func(gName, nName string, nIdx int) {
					api, err := mihomotui.GetMihomoAPI()
					if err != nil {
						return
					}
					if err := api.SelectProxy(gName, nName); err != nil {
						mihomotui.Warnf("选择代理失败: %v", err)
						return
					}
					app.QueueUpdateDraw(func() {
						selectedNode = nIdx
						refreshNodes()
					})
				}(group.Name, node.Name, idx)
			}, func() {
				// 单独测试该节点
				go func(nName string) {
					if selectedGroup >= len(proxyGroups) {
						return
					}
					g := proxyGroups[selectedGroup]
					var node *mihomotui.ProxyNode
					for i := range g.Nodes {
						if g.Nodes[i].Name == nName {
							node = &g.Nodes[i]
							break
						}
					}
					if node == nil || node.Type == "Direct" {
						return
					}
					node.Delay = -2
					app.QueueUpdateDraw(refreshNodes)

					api, err := mihomotui.GetMihomoAPI()
					if err != nil {
						node.Delay = -1
						app.QueueUpdateDraw(refreshNodes)
						return
					}
					delay, err := api.TestProxyDelayValue(nName, "https://www.gstatic.com/generate_204", 5000)
					if err != nil {
						node.Delay = -1
						app.QueueUpdateDraw(refreshNodes)
						return
					}
					if delay == 0 {
						node.Delay = -1
					} else {
						node.Delay = delay
					}
					app.QueueUpdateDraw(func() {
						sortNodes(g.Nodes)
						refreshNodes()
					})
				}(node.Name)
			})
			rowFlex.AddItem(card, 0, 1, false)
			count++
		}
		if rowFlex != nil {
			nodesFlex.AddItem(rowFlex, cardHeight, 0, false)
		}

		// 空列表提示
		if total == 0 {
			empty := tview.NewTextView().
				SetTextAlign(tview.AlignCenter).
				SetText("\n暂无节点")
			nodesFlex.AddItem(empty, 0, 1, false)
		}
	}

	// 批量延迟测试（使用 IPC group delay）
	var isTesting bool

	testBtn.SetSelectedFunc(func() {
		if selectedGroup >= len(proxyGroups) {
			return
		}
		group := &proxyGroups[selectedGroup]
		total := len(group.Nodes)
		if total == 0 {
			return
		}

		// 如果正在测试，点击则中断
		if isTesting {
			isTesting = false
			testBtn.SetLabel(" 测试延迟 ")
			for i := range group.Nodes {
				if group.Nodes[i].Delay == -2 {
					group.Nodes[i].Delay = -1
				}
			}
			refreshNodes()
			return
		}

		testBtn.SetLabel(" 中断测试 ")
		for i := range group.Nodes {
			if group.Nodes[i].Type != "Direct" {
				group.Nodes[i].Delay = -2 // 测试中
			}
		}
		refreshNodes()
		isTesting = true

		go func() {
			defer func() {
				isTesting = false
				app.QueueUpdateDraw(func() {
					testBtn.SetLabel(" 测试延迟 ")
				})
			}()

			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				app.QueueUpdateDraw(func() {
					for i := range group.Nodes {
						if group.Nodes[i].Delay == -2 {
							group.Nodes[i].Delay = -1
						}
					}
					refreshNodes()
				})
				return
			}

			delays, err := api.TestGroupDelayParsed(group.Name, "https://www.gstatic.com/generate_204", 5000)
			if err != nil {
				if !isTesting {
					return
				}
				mihomotui.Warnf("批量测试失败: %v", err)
				app.QueueUpdateDraw(func() {
					for i := range group.Nodes {
						if group.Nodes[i].Delay == -2 {
							group.Nodes[i].Delay = -1
						}
					}
					refreshNodes()
				})
				return
			}

			if !isTesting {
				return
			}

			app.QueueUpdateDraw(func() {
				for nodeName, d := range delays {
					for i := range group.Nodes {
						if group.Nodes[i].Name == nodeName {
							if d == 0 {
								group.Nodes[i].Delay = -1
							} else {
								group.Nodes[i].Delay = d
							}
							break
						}
					}
				}
				for i := range group.Nodes {
					if group.Nodes[i].Delay == -2 {
						group.Nodes[i].Delay = -1
					}
				}
				sortNodes(group.Nodes)
				refreshNodes()
			})
		}()
	})

	// 切换代理组
	groupDropdown.SetSelectedFunc(func(text string, index int) {
		selectedGroup = index
		selectedNode = 0
		currentPage = 0
		refreshNodes()
	})

	// 上一页
	prevBtn.SetSelectedFunc(func() {
		if currentPage > 0 {
			currentPage--
			refreshNodes()
		}
	})

	// 下一页
	nextBtn.SetSelectedFunc(func() {
		if currentPage < totalPages()-1 {
			currentPage++
			refreshNodes()
		}
	})

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(nodesFlex, 0, 1, false).
		AddItem(bottomBar, 1, 0, false)

	// 页面级键盘捕获：方向键在节点卡片间导航
	page.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() != page {
			return event
		}
		if selectedGroup >= len(proxyGroups) {
			return event
		}
		group := proxyGroups[selectedGroup]
		total := len(group.Nodes)

		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(groupDropdown)
			return nil
		case tcell.KeyRight:
			if selectedNode < total-1 {
				selectedNode++
				if selectedNode >= (currentPage+1)*maxPerPage {
					currentPage++
				}
				refreshNodes()
			}
			return nil
		case tcell.KeyLeft:
			if selectedNode > 0 {
				selectedNode--
				if selectedNode < currentPage*maxPerPage {
					currentPage--
				}
				refreshNodes()
			}
			return nil
		case tcell.KeyDown:
			if selectedNode+cardsPerRow < total {
				selectedNode += cardsPerRow
				if selectedNode >= (currentPage+1)*maxPerPage {
					currentPage = selectedNode / maxPerPage
				}
				refreshNodes()
			}
			return nil
		case tcell.KeyUp:
			if selectedNode-cardsPerRow >= 0 {
				selectedNode -= cardsPerRow
				if selectedNode < currentPage*maxPerPage {
					currentPage = selectedNode / maxPerPage
				}
				refreshNodes()
			}
			return nil
		case tcell.KeyEnter:
			// 代理页面 Enter 只是确认当前选中（已高亮），无需额外操作
			return nil
		}
		return event
	})

	// 自适应：根据终端宽高动态计算每页卡片数量和每行卡片数
	lastWidth, lastHeight := 0, 0
	page.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// 宽度自适应：每行卡片数
		newCardsPerRow := min(max(width/cardMinWidth, 1), 4)

		// 高度自适应：每页行数
		availableHeight := max(
			// toolbar(3) + bottomBar(1)
			height-4, cardHeight)
		rowsPerPage := max(availableHeight/cardHeight, 1)

		newMaxPerPage := rowsPerPage * newCardsPerRow

		// 只在变化时刷新，避免无限循环
		if width != lastWidth || height != lastHeight ||
			newMaxPerPage != maxPerPage || newCardsPerRow != cardsPerRow {
			lastWidth = width
			lastHeight = height
			maxPerPage = newMaxPerPage
			cardsPerRow = newCardsPerRow
			refreshNodes()
		}

		return x, y, width, height
	})

	// 加载代理数据（轮询，首次立即执行，之后每 5 秒刷新）
	go func() {
		for {
			api, err := mihomotui.GetMihomoAPI()
			if err == nil {
				groups, err := api.GetProxyGroups()
				if err == nil {
					app.QueueUpdateDraw(func() {
						proxyGroups = groups
						if len(proxyGroups) > 0 {
							opts := make([]string, len(proxyGroups))
							for i, g := range proxyGroups {
								opts[i] = g.Name
							}
							groupDropdown.SetOptions(opts, func(text string, index int) {
								selectedGroup = index
								selectedNode = 0
								currentPage = 0
								refreshNodes()
								// proxy 页面仅浏览节点，不修改默认代理策略配置
							})
							if selectedGroup < len(proxyGroups) {
								groupDropdown.SetCurrentOption(selectedGroup)
							} else {
								selectedGroup = 0
								selectedNode = 0
								currentPage = 0
								groupDropdown.SetCurrentOption(0)
							}
						}
						refreshNodes()
					})
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()

	refreshNodes()

	return page
}

// sortNodes 按延迟排序（低延迟优先，超时/未测试/测试中排最后）
func sortNodes(nodes []mihomotui.ProxyNode) {
	sort.Slice(nodes, func(i, j int) bool {
		di, dj := nodes[i].Delay, nodes[j].Delay
		if di == dj {
			return nodes[i].Name < nodes[j].Name
		}
		// 有效延迟（>=0）优先，按值升序（低延迟在前）
		if di >= 0 && dj >= 0 {
			return di < dj
		}
		// 有效延迟始终排在无效状态前面
		if di >= 0 {
			return true
		}
		if dj >= 0 {
			return false
		}
		// 无效状态内部：超时(-1) > 未测试(-3) > 测试中(-2)
		return di > dj
	})
}

// createProxyCard 创建代理节点卡片
func createProxyCard(node mihomotui.ProxyNode, selected bool, onClick func(), onTest func()) tview.Primitive {
	// 延迟颜色与文本
	delayColor := DelayColor(node.Delay)
	delayText := DelayText(node.Delay)

	// 信息文本
	var text string
	if selected {
		text = fmt.Sprintf(
			" [white::b]● %s[-:-:-]\n"+
				"    %s    [white]%s[-]",
			node.Name, node.Type, delayText,
		)
	} else {
		text = fmt.Sprintf(
			" [%s]●[-] [::b]%s[-:-:-]\n"+
				"    %s    [%s]%s[-]",
			delayColor, node.Name, node.Type, delayColor, delayText,
		)
	}

	info := tview.NewTextView().
		SetText(text).
		SetDynamicColors(true)

	// 单独测试按钮
	testBtn := tview.NewButton("测")
	testBtn.SetBorder(false)
	testBtn.SetSelectedFunc(onTest)

	// 卡片布局：信息 + 测试按钮
	card := tview.NewFlex().
		AddItem(info, 0, 1, false).
		AddItem(testBtn, 4, 0, true)
	card.SetBorder(true)

	if selected {
		card.SetBorderColor(tcell.ColorBlue)
		card.SetBorderAttributes(tcell.AttrBold)
		card.SetTitle(" ✓ ")
		card.SetTitleColor(tcell.ColorWhite)
		card.SetTitleAlign(tview.AlignLeft)
	}

	// 鼠标点击信息区域选中
	info.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick || action == tview.MouseLeftDown {
			onClick()
		}
		return action, event
	})

	return card
}
