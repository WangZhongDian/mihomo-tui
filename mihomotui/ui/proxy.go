package ui

import (
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
	"mihomotui/mihomotui"
)

// NewProxyPage 创建代理页面。
//
// 刷新策略（完全手动）：
//   - 进入页面、点「刷新」/按 r、点「测试延迟」、点单个节点「测」才会更新数据；
//   - 没有任何后台定时轮询，页面不会再自己跳动；
//   - 除「测试延迟」外，任何更新都不改变节点顺序、页码与光标；
//   - 渲染签名不变时完全不重绘。
func NewProxyPage(app *tview.Application) tview.Primitive {
	state := &proxyViewState{}
	var (
		ddSync    dropdownSync
		visible   = atomic.Bool{}
		loading   = atomic.Bool{}
		isTesting = atomic.Bool{}
		nodeTests = atomic.Int32{}
	)
	visible.Store(true)

	// 自适应参数
	maxPerPage := 4
	cardsPerRow := 2
	cardHeight := 4
	cardMinWidth := 24
	// 最近一次绘制时的可用宽度，用于按卡片实际宽度截断过长的节点名，
	// 避免名称换行后把类型/延迟那一行挤出卡片。
	pageWidth := 0

	// 与 mihomo 配置和 provider 健康检查使用同一测试地址。此前此页面
	// 硬编码 gstatic，在部分网络和新版内核下会导致测速失败而代理仍可用。
	testURL := mihomotui.GlobalConfig().Mihomo.TestURL
	if testURL == "" {
		testURL = "http://cp.cloudflare.com/generate_204"
	}

	// 顶部工具栏：代理组下拉框 + 测试延迟 + 刷新
	groupDropdown := tview.NewDropDown().
		SetLabel(" 代理组: ").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	testBtn := tview.NewButton(" 测试延迟 ")
	testBtn.SetBorder(false)

	refreshBtn := tview.NewButton(" 刷新 ")
	refreshBtn.SetBorder(false)

	toolbar := tview.NewFlex().
		AddItem(groupDropdown, 0, 1, false).
		AddItem(nil, 2, 0, false).
		AddItem(testBtn, 14, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(refreshBtn, 10, 0, false)

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

	// 先声明后赋值，解决 render 与各操作回调之间的循环引用。
	var (
		render            func(force bool)
		syncDropdown      func()
		refreshFromServer func()
		selectNode        func(groupName, nodeName string)
		testSingleNode    func(groupName, nodeName string)
		testAllNodes      func()
	)

	// nodeNameWidth 计算卡片内节点名的可用显示宽度（左右边框 2 列、
	// 右侧「测」按钮 4 列、首行「 ● 」前缀 3 列、右侧留白 1 列）。
	// 宽度未知（首帧）时返回 0，表示不截断。
	nodeNameWidth := func() int {
		if cardsPerRow <= 0 || pageWidth <= 0 {
			return 0
		}
		return max(pageWidth/cardsPerRow-10, 8)
	}

	// render 按当前视图状态重绘卡片。
	// force=false 时先比较渲染签名，内容完全一致则直接返回，避免无谓重建与闪烁。
	render = func(force bool) {
		nameWidth := nodeNameWidth()
		sig := state.signature(nameWidth, maxPerPage)
		if !force && sig == state.lastSig {
			return
		}
		state.lastSig = sig

		nodesFlex.Clear()
		group := state.currentGroup()
		if group == nil || len(group.Nodes) == 0 {
			pageInfo.SetText(" 1 / 1 ")
			empty := tview.NewTextView().
				SetTextAlign(tview.AlignCenter).
				SetText("\n暂无节点")
			nodesFlex.AddItem(empty, 0, 1, false)
			return
		}

		total := len(group.Nodes)
		state.clampPage(maxPerPage)
		start := state.page * maxPerPage
		end := min(start+maxPerPage, total)
		pageInfo.SetText(fmt.Sprintf(" %d / %d ", state.page+1, state.totalPages(maxPerPage)))

		var rowFlex *tview.Flex
		count := 0
		for i := start; i < end; i++ {
			if count%cardsPerRow == 0 {
				if rowFlex != nil {
					nodesFlex.AddItem(rowFlex, cardHeight, 0, false)
				}
				rowFlex = tview.NewFlex()
			}

			node := group.Nodes[i]
			// 回调只捕获名字（值拷贝），不捕获下标也不持有旧切片指针：
			// 节点顺序可能因测速重排而变，旧指针在刷新后也已失效。
			groupName, nodeName := group.Name, node.Name
			card := createProxyCard(node, nodeName == state.nodeName, nameWidth,
				func() { selectNode(groupName, nodeName) },
				func() { testSingleNode(groupName, nodeName) },
			)
			rowFlex.AddItem(card, 0, 1, false)
			count++
		}
		if rowFlex != nil {
			nodesFlex.AddItem(rowFlex, cardHeight, 0, false)
		}
	}

	// syncDropdown 静默同步策略组下拉框（不触发用户回调，不重置页码）。
	syncDropdown = func() {
		state.normalizeGroup()
		opts := make([]string, len(state.groups))
		for i, g := range state.groups {
			// 策略组名同样可能含国旗/方括号，按显示宽度归一化。
			opts[i] = SanitizeDisplayText(g.Name)
		}
		index := indexOfGroup(state.groups, state.groupName)
		if index < 0 {
			index = 0
		}
		ddSync.Sync(groupDropdown, opts, index, func(_ string, i int) {
			// 仅在用户真正切换策略组时执行：重置到该组的第一页。
			if i < 0 || i >= len(state.groups) {
				return
			}
			state.groupName = state.groups[i].Name
			state.nodeName = ""
			state.page = 0
			state.normalizeGroup()
			render(false)
		})
	}

	// refreshFromServer 主动拉取一次策略组数据并按保序方式合并。
	// 同一时刻只允许一个请求在飞行中，避免结果互相覆盖。
	refreshFromServer = func() {
		if !loading.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer loading.Store(false)
			// 先从 daemon 状态同步实际运行内核版本，再访问 mihomo REST API。
			// 这覆盖服务在 TUI 打开期间被 systemd 重启或切换版本的情况。
			if client, err := mihomotui.GetIPCClient(); err == nil {
				_, _ = client.IPCGetMihomoStatus()
			}
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				return
			}
			groups, err := api.GetProxyGroups()
			if err != nil {
				mihomotui.Warnf("获取代理组失败: %v", err)
				return
			}
			app.QueueUpdateDraw(func() {
				if !visible.Load() {
					return
				}
				state.applyServerGroups(groups)
				syncDropdown()
				render(false)
			})
		}()
	}

	// selectNode 选中节点：只改变高亮，不重排节点、不翻页。
	selectNode = func(groupName, nodeName string) {
		state.nodeName = nodeName
		render(false)
		go func() {
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				return
			}
			if err := api.SelectProxy(groupName, nodeName); err != nil {
				mihomotui.Warnf("选择代理失败: %v", err)
			}
		}()
	}

	// testSingleNode 测试单个节点。结果按名称写回，并让光标跟随该节点
	// （重排后仍指向同一个节点，不会跳回第一个）。
	testSingleNode = func(groupName, nodeName string) {
		if isTesting.Load() {
			return // 批量测速进行中，忽略单节点测试，避免结果互相覆盖
		}
		nodeTests.Add(1)
		if state.setNodeDelay(groupName, nodeName, mihomotui.DelayTesting) {
			render(false)
		}
		go func() {
			// 计数必须在协程结束时释放，否则启动瞬间就会被减回去。
			defer nodeTests.Add(-1)
			delay := mihomotui.DelayTimeout
			api, err := mihomotui.GetMihomoAPI()
			if err == nil {
				d, derr := api.TestProxyDelayValue(nodeName, testURL, 5000)
				switch {
				case derr != nil, d == 0:
					delay = mihomotui.DelayTimeout
				default:
					delay = d
				}
			}
			app.QueueUpdateDraw(func() {
				if !visible.Load() {
					return
				}
				if state.setNodeDelay(groupName, nodeName, delay) {
					state.sortAndFollow(maxPerPage)
				}
				render(false)
			})
		}()
	}

	// testAllNodes 批量测速。完成后按延迟重排，并让页码跟随原选中节点。
	testAllNodes = func() {
		group := state.currentGroup()
		if group == nil || len(group.Nodes) == 0 {
			return
		}
		groupName := group.Name

		// 正在测试时点击 = 中断：把「测试中」收尾为超时。
		if isTesting.Load() {
			isTesting.Store(false)
			testBtn.SetLabel(" 测试延迟 ")
			state.resolveTesting(groupName, mihomotui.DelayTimeout)
			render(false)
			return
		}
		if nodeTests.Load() > 0 {
			return // 有单节点测试在进行，避免状态互相干扰
		}
		isTesting.Store(true)
		testBtn.SetLabel(" 中断测试 ")
		state.markGroupTesting(groupName)
		render(false)

		go func() {
			defer func() {
				app.QueueUpdateDraw(func() {
					// 用户可能已经中断并重新开始测试，此时不要把按钮改回默认。
					if !isTesting.Load() {
						testBtn.SetLabel(" 测试延迟 ")
					}
				})
			}()
			delays, err := func() (map[string]int, error) {
				api, aerr := mihomotui.GetMihomoAPI()
				if aerr != nil {
					return nil, aerr
				}
				return api.TestGroupDelayParsed(groupName, testURL, 5000)
			}()

			app.QueueUpdateDraw(func() {
				if !visible.Load() {
					isTesting.Store(false)
					return
				}
				// 用户中断或批量测试失败：把「测试中」收尾，不写回任何结果。
				if !isTesting.Load() {
					state.resolveTesting(groupName, mihomotui.DelayTimeout)
					render(false)
					return
				}
				isTesting.Store(false)
				if err != nil {
					mihomotui.Warnf("批量测试失败: %v", err)
					state.resolveTesting(groupName, mihomotui.DelayTimeout)
					render(false)
					return
				}
				state.applyDelays(groupName, delays)
				// 其余未返回结果的节点按超时收尾。
				state.resolveTesting(groupName, mihomotui.DelayTimeout)
				state.sortAndFollow(maxPerPage)
				render(false)
			})
		}()
	}

	testBtn.SetSelectedFunc(testAllNodes)
	refreshBtn.SetSelectedFunc(refreshFromServer)

	// 上一页 / 下一页：只改变页码，不改动顺序与其他状态。
	prevBtn.SetSelectedFunc(func() {
		if state.page > 0 {
			state.page--
			render(false)
		}
	})
	nextBtn.SetSelectedFunc(func() {
		if state.page < state.totalPages(maxPerPage)-1 {
			state.page++
			render(false)
		}
	})

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, false).
		AddItem(nodesFlex, 0, 1, false).
		AddItem(bottomBar, 1, 0, false)

	// 页面级键盘捕获：方向键在节点卡片间导航，r 手动刷新。
	page.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if app.GetFocus() != page {
			return event
		}
		group := state.currentGroup()
		if group == nil {
			return event
		}
		total := len(group.Nodes)
		cur := indexOfNode(group.Nodes, state.nodeName)

		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(groupDropdown)
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'r', 'R':
				refreshFromServer()
				return nil
			}
			return event
		case tcell.KeyRight:
			if cur >= 0 && cur < total-1 {
				state.nodeName = group.Nodes[cur+1].Name
				if cur+1 >= (state.page+1)*maxPerPage {
					state.page++
				}
				render(false)
			}
			return nil
		case tcell.KeyLeft:
			if cur > 0 {
				state.nodeName = group.Nodes[cur-1].Name
				if cur-1 < state.page*maxPerPage {
					state.page--
				}
				render(false)
			}
			return nil
		case tcell.KeyDown:
			if cur >= 0 && cur+cardsPerRow < total {
				next := cur + cardsPerRow
				state.nodeName = group.Nodes[next].Name
				if next >= (state.page+1)*maxPerPage {
					state.page = next / maxPerPage
				}
				render(false)
			}
			return nil
		case tcell.KeyUp:
			if cur-cardsPerRow >= 0 {
				next := cur - cardsPerRow
				state.nodeName = group.Nodes[next].Name
				if next < state.page*maxPerPage {
					state.page = next / maxPerPage
				}
				render(false)
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
			pageWidth = width
			maxPerPage = newMaxPerPage
			cardsPerRow = newCardsPerRow
			// 终端尺寸变化会改变渲染签名，因此这里必须强制重绘。
			render(true)
		}

		return x, y, width, height
	})

	render(true)

	return &proxyPage{
		Primitive: page,
		onShow: func() {
			visible.Store(true)
			// 每次进入页面拉取一次最新数据（取代原来的定时轮询）。
			refreshFromServer()
		},
		onStop: func() { visible.Store(false) },
	}
}

// proxyPage 包装代理页并实现 Page / Shower 生命周期：
// 切到其他页面时停止接收数据，切回时立即刷新一次。
// 这也取代了原先永不停止的后台轮询 goroutine。
type proxyPage struct {
	tview.Primitive
	onShow func()
	onStop func()
}

// OnShow 页面被切换为可见时调用。
func (p *proxyPage) OnShow() {
	if p.onShow != nil {
		p.onShow()
	}
}

// Stop 页面被切走时调用。
func (p *proxyPage) Stop() {
	if p.onStop != nil {
		p.onStop()
	}
}

// sortNodes 按延迟排序（低延迟优先，超时/未测试/测试中排最后）。
// 使用稳定排序，保证相同延迟的节点相对顺序不变，避免卡片无谓换位。
func sortNodes(nodes []mihomotui.ProxyNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return delayLess(nodes[i], nodes[j])
	})
}

// delayLess 定义延迟排序的比较规则：有效延迟（>=0）按值升序排在最前，
// 无效状态内部按 超时(-1) > 未测试(-3) > 测试中(-2) 排列。
func delayLess(a, b mihomotui.ProxyNode) bool {
	di, dj := a.Delay, b.Delay
	if di == dj {
		return a.Name < b.Name
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
}

// cardMetaLine 生成卡片第二行（类型 + 延迟）。
// nameWidth 是节点名可用宽度，卡片内容宽度约为 nameWidth+4；
// 卡片较窄时先收紧间距、再截断类型，始终保证延迟完整可见——
// 延迟是本页面的核心信息，不能被类型挤掉。
func cardMetaLine(typeName, delayText string, nameWidth int) string {
	const indent = "    "
	if nameWidth <= 0 {
		// 宽度未知（首帧）：保持原布局，由终端裁剪。
		return indent + typeName + "    " + delayText
	}
	contentWidth := nameWidth + 4
	if w := len(indent) + uniseg.StringWidth(typeName) + len(indent) + uniseg.StringWidth(delayText); w <= contentWidth {
		return indent + typeName + indent + delayText
	}
	budget := contentWidth - len(indent) - 1 - uniseg.StringWidth(delayText)
	if budget < 4 {
		budget = 4
	}
	return indent + TruncateDisplay(typeName, budget) + " " + delayText
}

// createProxyCard 创建代理节点卡片。
// nameWidth 是节点名可用的显示宽度（0 表示按终端裁剪不预处理），
// 超过该宽度的名称会被截断，保证类型/延迟行始终可见。
func createProxyCard(node mihomotui.ProxyNode, selected bool, nameWidth int, onClick func(), onTest func()) tview.Primitive {
	// 延迟颜色与文本
	delayColor := DelayColor(node.Delay)
	delayText := DelayText(node.Delay)

	// 节点名来自订阅，可能含国旗 emoji、方括号或超长中文：
	// 必须归一化显示宽度并转义 tview 标签，否则会出现起始字符重叠或文字被吞。
	displayName := DisplayName(node.Name, nameWidth)
	displayType := SanitizeDisplayText(node.Type)
	metaLine := cardMetaLine(displayType, delayText, nameWidth)

	// 信息文本
	var text string
	if selected {
		text = fmt.Sprintf(
			" [white::b]● %s[-:-:-]\n%s",
			displayName, metaLine,
		)
	} else {
		text = fmt.Sprintf(
			" [%s]●[-] [::b]%s[-:-:-]\n[%s]%s[-]",
			delayColor, displayName, delayColor, metaLine,
		)
	}

	info := tview.NewTextView().
		SetText(text).
		SetDynamicColors(true).
		// 关闭自动换行：长名称按卡片宽度裁剪（已提前截断），
		// 不再把第二行类型/延迟挤到卡片外。
		SetWrap(false)

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
