package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// testGroup 构造一个测试用策略组。
func testGroup(name, now string, nodes ...mihomotui.ProxyNode) mihomotui.ProxyGroup {
	return mihomotui.ProxyGroup{Name: name, Type: "Selector", Now: now, Nodes: nodes}
}

func node(name string, delay int) mihomotui.ProxyNode {
	return mihomotui.ProxyNode{Name: name, Type: "Shadowsocks", Delay: delay}
}

func nodeNames(nodes []mihomotui.ProxyNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}

// 服务端按延迟重新排序后，合并结果必须保持客户端已有顺序，
// 否则卡片位置会随每次刷新漂移。
func TestMergeProxyNodesPreservesOrder(t *testing.T) {
	existing := []mihomotui.ProxyNode{node("A", 300), node("B", 100), node("C", 500)}
	// 服务端按延迟升序返回，顺序完全不同。
	incoming := []mihomotui.ProxyNode{node("B", 90), node("A", 310), node("C", 520), node("D", 60)}

	got := mergeProxyNodes(existing, incoming)
	if names := nodeNames(got); strings.Join(names, ",") != "A,B,C,D" {
		t.Fatalf("顺序应保持为 A,B,C 并追加 D，实际 %v", names)
	}
	// 延迟必须被更新为远端最新值。
	if got[0].Delay != 310 || got[1].Delay != 90 || got[2].Delay != 520 || got[3].Delay != 60 {
		t.Fatalf("延迟未更新: %+v", got)
	}
}

// 远端不再出现的节点要被移除，而不是继续显示过期数据。
func TestMergeProxyNodesDropsVanishedNodes(t *testing.T) {
	existing := []mihomotui.ProxyNode{node("A", 100), node("GONE", 200)}
	incoming := []mihomotui.ProxyNode{node("A", 110)}

	got := mergeProxyNodes(existing, incoming)
	if names := nodeNames(got); strings.Join(names, ",") != "A" {
		t.Fatalf("消失的节点应被移除，实际 %v", names)
	}
}

// 「测试中」是本地临时状态，不能被远端返回的旧延迟覆盖。
func TestMergeProxyNodesKeepsTestingState(t *testing.T) {
	existing := []mihomotui.ProxyNode{node("A", mihomotui.DelayTesting)}
	incoming := []mihomotui.ProxyNode{node("A", 123)}

	got := mergeProxyNodes(existing, incoming)
	if got[0].Delay != mihomotui.DelayTesting {
		t.Fatalf("测试中状态被覆盖为 %d", got[0].Delay)
	}
}

// 首次加载（本地无数据）直接采用服务端顺序。
func TestMergeProxyNodesFirstLoadUsesServerOrder(t *testing.T) {
	incoming := []mihomotui.ProxyNode{node("B", 90), node("A", 310)}
	got := mergeProxyNodes(nil, incoming)
	if names := nodeNames(got); strings.Join(names, ",") != "B,A" {
		t.Fatalf("首次加载应采用服务端顺序，实际 %v", names)
	}
}

// 渲染签名只反映影响画面的因素：数据不变则签名不变（可跳过重绘）。
func TestProxyRenderSignatureStable(t *testing.T) {
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G", "A", node("A", 100), node("B", 200))})

	base := state.signature(20, 4)
	if again := state.signature(20, 4); again != base {
		t.Fatal("相同状态的签名必须一致")
	}

	// 延迟变化 → 签名变化（需要重绘）。
	state.setNodeDelay("G", "A", 111)
	if changed := state.signature(20, 4); changed == base {
		t.Fatal("延迟变化后签名应不同")
	}

	// 页码变化 → 签名变化。
	state = &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G", "A", node("A", 100), node("B", 200))})
	p0 := state.signature(20, 4)
	state.page = 1
	if p1 := state.signature(20, 4); p1 == p0 {
		t.Fatal("页码变化后签名应不同")
	}

	// 宽度变化 → 签名变化（终端缩放需要重排）。
	state = &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G", "A", node("A", 100))})
	if state.signature(20, 4) == state.signature(30, 4) {
		t.Fatal("宽度变化后签名应不同")
	}
}

// 复现原缺陷：用户停在第 3 页、光标在第 7 个节点，
// 后台同步（服务端重排 + 延迟变化）后页码与光标必须保持不变。
func TestApplyServerGroupsKeepsPageAndSelection(t *testing.T) {
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{
		testGroup("G", "n4",
			node("n1", 100), node("n2", 110), node("n3", 120), node("n4", 130),
			node("n5", 140), node("n6", 150), node("n7", 160), node("n8", 170)),
	})
	state.page = 1 // 第二页（每页 4 个）
	state.nodeName = "n7"

	// 服务端按延迟重排，顺序完全打乱，且延迟有变化。
	state.applyServerGroups([]mihomotui.ProxyGroup{
		testGroup("G", "n4",
			node("n8", 90), node("n7", 95), node("n6", 96), node("n5", 97),
			node("n4", 98), node("n3", 99), node("n2", 100), node("n1", 101)),
	})

	if state.page != 1 {
		t.Fatalf("页码被重置为 %d，应保持 1", state.page)
	}
	if state.nodeName != "n7" {
		t.Fatalf("光标被重置为 %q，应保持 n7", state.nodeName)
	}
	// 顺序保持客户端原有顺序（n1..n8）。
	if names := nodeNames(state.currentGroup().Nodes); names[0] != "n1" || names[7] != "n8" {
		t.Fatalf("节点顺序发生漂移: %v", names)
	}
}

// tview 的 SetCurrentOption 会触发选项回调；静默同步必须挡住它，
// 且连续同步相同选项时用户回调一次都不应被调用。
func TestSyncGroupDropdownDoesNotFireUserHandler(t *testing.T) {
	dd := tview.NewDropDown()
	sync := &dropdownSync{}

	calls := 0
	handler := func(_ string, _ int) { calls++ }
	opts := []string{"Auto", "Manual"}

	if !sync.Sync(dd, opts, 0, handler) {
		t.Fatal("首次同步应重建选项")
	}
	if rebuilt := sync.Sync(dd, opts, 1, handler); rebuilt {
		t.Fatal("选项未变化时不应重建")
	}
	if calls != 0 {
		t.Fatalf("静默同步触发了 %d 次用户回调（正是弹回第一页的原因）", calls)
	}
	if idx, text := dd.GetCurrentOption(); idx != 1 || text != "Manual" {
		t.Fatalf("下拉框选中项未同步: %d %q", idx, text)
	}

	// 真实用户切换仍必须生效。
	dd.SetCurrentOption(0)
	if calls != 1 {
		t.Fatalf("用户切换未触发回调，calls=%d", calls)
	}
}

// 选项变化时重建下拉框，重建过程同样不能触发用户回调。
func TestSyncGroupDropdownRebuildDoesNotFire(t *testing.T) {
	dd := tview.NewDropDown()
	sync := &dropdownSync{}
	calls := 0
	handler := func(_ string, _ int) { calls++ }

	sync.Sync(dd, []string{"A"}, 0, handler)
	sync.Sync(dd, []string{"A", "B"}, 1, handler)
	if calls != 0 {
		t.Fatalf("重建选项触发了 %d 次用户回调", calls)
	}
	if cnt := dd.GetOptionCount(); cnt != 2 {
		t.Fatalf("选项数量 = %d, want 2", cnt)
	}
}

// 当前策略组被删除时安全回退到第一个组，不 panic、不残留旧光标。
func TestGroupVanishedFallsBackSafely(t *testing.T) {
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{
		testGroup("G1", "", node("a", 1)),
		testGroup("G2", "", node("b", 2)),
	})
	state.groupName = "G2"
	state.nodeName = "b"
	state.page = 1

	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G1", "", node("a", 3))})

	if state.groupName != "G1" {
		t.Fatalf("组消失后应回退到第一个组，实际 %q", state.groupName)
	}
	if state.page != 0 {
		t.Fatalf("组消失后页码应归零，实际 %d", state.page)
	}
	if state.nodeName != "a" {
		t.Fatalf("光标应指向新组第一个节点，实际 %q", state.nodeName)
	}
}

// 测速重排后，光标与页码都要跟随原来选中的节点。
func TestSortAndFollowCursor(t *testing.T) {
	nodes := []mihomotui.ProxyNode{
		node("n1", 900), node("n2", 800), node("n3", 700), node("n4", 600),
		node("n5", 500), node("n6", 100), node("n7", 400), node("n8", 300),
	}
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G", "", nodes...)})
	state.nodeName = "n6" // 原来在第二页（索引 5，每页 4 个）
	state.page = 1

	state.sortAndFollow(4)

	if state.currentGroup().Nodes[0].Name != "n6" {
		t.Fatalf("最快节点应排到首位，实际 %q", state.currentGroup().Nodes[0].Name)
	}
	if state.nodeName != "n6" {
		t.Fatalf("光标丢失，实际 %q", state.nodeName)
	}
	if state.page != 0 {
		t.Fatalf("光标所在页应为 0，实际 %d", state.page)
	}
}

// 单节点测速结果按名称写回，且重排后仍能定位到同一节点。
func TestNodeDelayAppliedByName(t *testing.T) {
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{
		testGroup("G", "", node("A", 100), node("B", 200)),
	})

	// 模拟重排（B 变快）。
	if !state.setNodeDelay("G", "B", 50) {
		t.Fatal("按名称写回失败")
	}
	state.sortAndFollow(4)
	if names := nodeNames(state.currentGroup().Nodes); strings.Join(names, ",") != "B,A" {
		t.Fatalf("重排结果异常: %v", names)
	}

	// 重排之后再写回 A，仍应命中正确的节点。
	if !state.setNodeDelay("G", "A", 60) {
		t.Fatal("重排后按名称写回失败")
	}
	group := state.currentGroup()
	if group.Nodes[indexOfNode(group.Nodes, "A")].Delay != 60 {
		t.Fatalf("A 的延迟未更新: %+v", group.Nodes)
	}
	// 不存在的节点不应影响任何数据。
	if state.setNodeDelay("G", "missing", 1) {
		t.Fatal("不存在的节点不应返回 true")
	}
}

// 批量测速把「测试中」收尾为超时，不会残留永久「测试中」状态。
func TestResolveTestingFallsBackToTimeout(t *testing.T) {
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{
		testGroup("G", "", node("A", 100), node("B", 200)),
	})
	state.markGroupTesting("G")
	for _, n := range state.currentGroup().Nodes {
		if n.Delay != mihomotui.DelayTesting {
			t.Fatalf("节点未标记为测试中: %+v", n)
		}
	}
	state.applyDelays("G", map[string]int{"A": 88})
	state.resolveTesting("G", mihomotui.DelayTimeout)

	group := state.currentGroup()
	if group.Nodes[indexOfNode(group.Nodes, "A")].Delay != 88 {
		t.Fatalf("已返回结果的节点不应被收尾: %+v", group.Nodes)
	}
	if group.Nodes[indexOfNode(group.Nodes, "B")].Delay != mihomotui.DelayTimeout {
		t.Fatalf("未返回结果的节点应收尾为超时: %+v", group.Nodes)
	}
}

// 页码只在越界时收敛到最后有效页，不会归零。
func TestClampPageKeepsValidPage(t *testing.T) {
	nodes := make([]mihomotui.ProxyNode, 9)
	for i := range nodes {
		nodes[i] = node(string(rune('a'+i)), 100)
	}
	state := &proxyViewState{}
	state.applyServerGroups([]mihomotui.ProxyGroup{testGroup("G", "", nodes...)})

	state.page = 2 // 9 个节点、每页 4 个 → 3 页，页码 2 合法
	state.clampPage(4)
	if state.page != 2 {
		t.Fatalf("合法页码被改动: %d", state.page)
	}

	state.page = 5 // 越界
	state.clampPage(4)
	if state.page != 2 {
		t.Fatalf("越界页码应收敛到最后一页 2，实际 %d", state.page)
	}
}

// 冒烟测试：页面能在模拟终端上完整绘制，并实现 Page / Shower 生命周期。
func TestProxyPageDrawsAndImplementsLifecycle(t *testing.T) {
	useTestLanguage(t, "zh-CN")
	app := tview.NewApplication()
	page := NewProxyPage(app)

	if _, ok := page.(Page); !ok {
		t.Fatal("代理页必须实现 Page（切走时停止接收数据）")
	}
	if _, ok := page.(Shower); !ok {
		t.Fatal("代理页必须实现 Shower（切回时刷新一次）")
	}

	lines := renderPrimitive(t, page, 100, 24)
	if len(lines) != 24 {
		t.Fatalf("渲染行数 = %d, want 24", len(lines))
	}
	// 空数据时给出明确提示，而不是空白页。
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "暂无节点") {
		t.Fatalf("空状态提示缺失:\n%s", joined)
	}
	// 工具栏与分页栏就位。
	if !strings.Contains(joined, "代理组") || !strings.Contains(joined, "测试延迟") || !strings.Contains(joined, "刷新") {
		t.Fatalf("工具栏控件缺失:\n%s", joined)
	}
	if !strings.Contains(joined, "1 / 1") {
		t.Fatalf("分页信息缺失:\n%s", joined)
	}
}

// 生命周期钩子不应 panic，并能在 Stop 后安全地丢弃迟到的数据。
func TestProxyPageLifecycleHooks(t *testing.T) {
	app := tview.NewApplication()
	page := NewProxyPage(app)
	p := page.(*proxyPage)
	p.OnShow()
	p.Stop()
	p.OnShow() // 再切回不应 panic
}
