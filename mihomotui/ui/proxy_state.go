package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// proxyViewState 保存代理页的视图状态。
//
// 关键不变式（这是「翻页后被弹回第一页」的修复核心）：
//   - 数据同步只更新 Nodes 的内容（延迟、类型），不改变节点顺序；
//   - 数据同步不修改 groupName / nodeName / page；
//   - 只有「测试延迟」会重排节点，并让页码与光标跟随原来选中的节点。
//
// 组和节点都用名字而不是下标来记录，避免列表顺序变化后下标指向了别的对象。
type proxyViewState struct {
	groups    []mihomotui.ProxyGroup
	groupName string // 当前策略组名
	nodeName  string // 当前光标所在节点名
	page      int    // 当前页码（0 起）
	lastSig   string // 上次渲染签名，用于脏检查
}

// indexOfGroup 按名称查找策略组下标，不存在返回 -1。
func indexOfGroup(groups []mihomotui.ProxyGroup, name string) int {
	for i := range groups {
		if groups[i].Name == name {
			return i
		}
	}
	return -1
}

// indexOfNode 按名称查找节点下标，不存在返回 -1。
func indexOfNode(nodes []mihomotui.ProxyNode, name string) int {
	for i := range nodes {
		if nodes[i].Name == name {
			return i
		}
	}
	return -1
}

// currentGroup 返回当前策略组；组列表为空时返回 nil。
func (s *proxyViewState) currentGroup() *mihomotui.ProxyGroup {
	if len(s.groups) == 0 {
		return nil
	}
	if i := indexOfGroup(s.groups, s.groupName); i >= 0 {
		return &s.groups[i]
	}
	return &s.groups[0]
}

// normalizeGroup 让 groupName/nodeName/page 指向真实存在的对象。
// 组被删除时回退到第一个组并清空光标；仅在这种情况下才重置页码。
func (s *proxyViewState) normalizeGroup() {
	if len(s.groups) == 0 {
		s.groupName = ""
		s.nodeName = ""
		s.page = 0
		return
	}
	if indexOfGroup(s.groups, s.groupName) < 0 {
		s.groupName = s.groups[0].Name
		s.nodeName = ""
		s.page = 0
	}
	group := s.currentGroup()
	if group == nil {
		return
	}
	// 光标节点消失时退回该组第一个节点，但不影响页码之外的状态。
	if indexOfNode(group.Nodes, s.nodeName) < 0 {
		if len(group.Nodes) > 0 {
			s.nodeName = group.Nodes[0].Name
		} else {
			s.nodeName = ""
		}
	}
}

// totalPages 返回当前组的总页数（至少 1）。
func (s *proxyViewState) totalPages(perPage int) int {
	group := s.currentGroup()
	if group == nil || perPage <= 0 || len(group.Nodes) == 0 {
		return 1
	}
	return (len(group.Nodes) + perPage - 1) / perPage
}

// clampPage 仅在页码越界（节点变少或终端变矮）时收敛到最后有效页，
// 不会像原来的实现那样把页码归零。
func (s *proxyViewState) clampPage(perPage int) {
	tp := s.totalPages(perPage)
	if s.page >= tp {
		s.page = tp - 1
	}
	if s.page < 0 {
		s.page = 0
	}
}

// mergeProxyNodes 保序合并节点列表。
//
// 以 existing 的顺序为准：incoming 只用于更新同名节点的延迟与类型；
// incoming 中新增的节点按服务端顺序追加到末尾；不再存在的节点被移除。
// 这样后台/手动刷新都不会让卡片位置漂移。
func mergeProxyNodes(existing, incoming []mihomotui.ProxyNode) []mihomotui.ProxyNode {
	if len(incoming) == 0 {
		return nil
	}
	if len(existing) == 0 {
		return append([]mihomotui.ProxyNode(nil), incoming...)
	}
	byName := make(map[string]mihomotui.ProxyNode, len(incoming))
	for _, node := range incoming {
		byName[node.Name] = node
	}
	out := make([]mihomotui.ProxyNode, 0, len(incoming))
	seen := make(map[string]bool, len(incoming))
	for _, old := range existing {
		fresh, ok := byName[old.Name]
		if !ok || seen[old.Name] {
			continue
		}
		seen[old.Name] = true
		// 「测试中」是本地临时状态，不能被远端旧数据覆盖掉。
		if old.Delay == mihomotui.DelayTesting {
			fresh.Delay = old.Delay
		}
		out = append(out, fresh)
	}
	for _, node := range incoming {
		if seen[node.Name] {
			continue
		}
		seen[node.Name] = true
		out = append(out, node)
	}
	return out
}

// applyServerGroups 把服务端返回的组列表合并进视图状态。
// 页码、当前组和光标都保持不变（组被删除时除外）。
func (s *proxyViewState) applyServerGroups(incoming []mihomotui.ProxyGroup) {
	merged := make([]mihomotui.ProxyGroup, 0, len(incoming))
	for _, in := range incoming {
		out := in
		if i := indexOfGroup(s.groups, in.Name); i >= 0 {
			// 已有组：前 4 个字段（Name/Type/Now/Nodes）中只有 Now 和 Nodes 内容会变。
			out.Nodes = mergeProxyNodes(s.groups[i].Nodes, in.Nodes)
		}
		merged = append(merged, out)
	}
	s.groups = merged
	s.normalizeGroup()
}

// setNodeDelay 按名称写回单个节点的延迟，返回是否找到该节点。
func (s *proxyViewState) setNodeDelay(groupName, nodeName string, delay int) bool {
	i := indexOfGroup(s.groups, groupName)
	if i < 0 {
		return false
	}
	j := indexOfNode(s.groups[i].Nodes, nodeName)
	if j < 0 {
		return false
	}
	s.groups[i].Nodes[j].Delay = delay
	return true
}

// markGroupTesting 把组内非直连节点标记为「测试中」。
func (s *proxyViewState) markGroupTesting(groupName string) {
	i := indexOfGroup(s.groups, groupName)
	if i < 0 {
		return
	}
	for j := range s.groups[i].Nodes {
		if s.groups[i].Nodes[j].Type != "Direct" {
			s.groups[i].Nodes[j].Delay = mihomotui.DelayTesting
		}
	}
}

// resolveTesting 把仍是「测试中」的节点落回指定状态。
// 批量测速未返回结果的节点、被中断的测速都用它收尾。
func (s *proxyViewState) resolveTesting(groupName string, fallback int) {
	i := indexOfGroup(s.groups, groupName)
	if i < 0 {
		return
	}
	for j := range s.groups[i].Nodes {
		if s.groups[i].Nodes[j].Delay == mihomotui.DelayTesting {
			s.groups[i].Nodes[j].Delay = fallback
		}
	}
}

// applyDelays 按节点名写回批量测速结果。delay==0 表示该节点超时。
// 返回实际写入的节点数（用于日志与测试）。
func (s *proxyViewState) applyDelays(groupName string, delays map[string]int) int {
	i := indexOfGroup(s.groups, groupName)
	if i < 0 {
		return 0
	}
	applied := 0
	for j := range s.groups[i].Nodes {
		d, ok := delays[s.groups[i].Nodes[j].Name]
		if !ok {
			continue
		}
		if d == 0 {
			s.groups[i].Nodes[j].Delay = mihomotui.DelayTimeout
		} else {
			s.groups[i].Nodes[j].Delay = d
		}
		applied++
	}
	return applied
}

// sortAndFollow 按延迟重排当前组，并把页码跟随到原本选中的节点。
// 这是唯一允许改变节点顺序的入口（仅由用户主动测速触发）。
func (s *proxyViewState) sortAndFollow(perPage int) {
	group := s.currentGroup()
	if group == nil {
		return
	}
	name := s.nodeName
	sortNodes(group.Nodes)
	if name == "" || perPage <= 0 {
		s.page = 0
		return
	}
	if idx := indexOfNode(group.Nodes, name); idx >= 0 {
		s.page = idx / perPage
	}
}

// signature 汇总所有影响渲染的输入：签名不变说明画面完全一致，
// 可以跳过整个重绘（既避免闪烁，也保证「内容没变就不打扰用户」）。
func (s *proxyViewState) signature(nameWidth, perPage int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "w=%d;p=%d;pp=%d;g=%q;n=%q;", nameWidth, s.page, perPage, s.groupName, s.nodeName)
	group := s.currentGroup()
	if group == nil {
		return sb.String()
	}
	sb.WriteString(group.Name)
	sb.WriteByte('|')
	sb.WriteString(group.Now)
	for _, node := range group.Nodes {
		sb.WriteByte('|')
		sb.WriteString(node.Name)
		sb.WriteByte(':')
		sb.WriteString(node.Type)
		fmt.Fprintf(&sb, ":%d", node.Delay)
	}
	return sb.String()
}

// dropdownSync 在不触发用户回调的前提下同步下拉框选项。
//
// 背景：tview 的 DropDown.SetCurrentOption 会调用选项回调（dropdown.go），
// 旧实现每次同步都调用它，而回调里重置了 currentPage，
// 导致每 5 秒把用户翻到的第 3 页弹回第 1 页。
type dropdownSync struct {
	last     []string
	suppress bool
}

// Sync 用给定选项静默更新下拉框并选中 index，返回选项列表是否被重建。
func (s *dropdownSync) Sync(dd *tview.DropDown, opts []string, index int, handler func(string, int)) bool {
	rebuilt := !s.sameOptions(opts)
	if rebuilt {
		dd.SetOptions(opts, nil)
		s.last = append([]string(nil), opts...)
	}
	// 用户回调统一套一层守卫，即使将来 tview 在设置选项时再次触发回调，
	// 也不会改动页码/光标等视图状态。
	dd.SetSelectedFunc(func(text string, i int) {
		if s.suppress || handler == nil {
			return
		}
		handler(text, i)
	})
	s.suppress = true
	dd.SetCurrentOption(index)
	s.suppress = false
	return rebuilt
}

// sameOptions 比较本次与上次的选项文本是否一致。
func (s *dropdownSync) sameOptions(opts []string) bool {
	if len(s.last) != len(opts) {
		return false
	}
	for i := range opts {
		if s.last[i] != opts[i] {
			return false
		}
	}
	return true
}
