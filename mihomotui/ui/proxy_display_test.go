package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
	"mihomotui/mihomotui"
)

// renderPrimitive 把任意 tview 组件绘制到指定尺寸的模拟屏幕上，返回每行文本。
func renderPrimitive(t *testing.T, p interface {
	SetRect(int, int, int, int)
	Draw(tcell.Screen)
}, width, height int) []string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	p.SetRect(0, 0, width, height)
	p.Draw(screen)
	screen.Show()
	cells, w, h := screen.GetContents()
	lines := make([]string, 0, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			cell := cells[y*w+x]
			if len(cell.Runes) == 0 {
				sb.WriteRune(' ')
				continue
			}
			content := string(cell.Runes)
			sb.WriteString(content)
			x += uniseg.StringWidth(content) - 1
		}
		lines = append(lines, strings.TrimRight(sb.String(), " "))
	}
	return lines
}

// 回归：带国旗 emoji 的超长节点名不能把类型/延迟行挤出卡片，
// 首行也不能再输出区域指示符（不同终端上会渲染成 4 格导致起始字符重叠）。
func TestProxyCardLongFlagNameKeepsDelayLine(t *testing.T) {
	node := mihomotui.ProxyNode{
		Name:  "🇯🇵AWS日本06 | 电信移动联通推荐",
		Type:  "Shadowsocks",
		Delay: 123,
	}
	card := createProxyCard(node, false, 22, func() {}, func() {})
	lines := renderPrimitive(t, card, 32, 4)

	if strings.ContainsRune(strings.Join(lines, ""), '🇯') {
		t.Fatalf("卡片仍在输出区域指示符（会导致起始字符重叠）: %q", lines)
	}
	if !strings.Contains(lines[1], "JPAWS") {
		t.Fatalf("国旗未转换为字母，首行内容异常: %q", lines[1])
	}
	// 第二行必须保住类型和延迟，说明名称没有换行占位。
	if !strings.Contains(lines[2], "Shadowsocks") || !strings.Contains(lines[2], "123ms") {
		t.Fatalf("超长名称挤掉了类型/延迟行: %q", lines)
	}
	for _, line := range lines {
		if width := uniseg.StringWidth(line); width > 32 {
			t.Fatalf("行宽 %d 超出卡片宽度: %q", width, line)
		}
	}
}

// 回归：名称里的方括号必须原样显示，不能被当作 tview 标签吞掉。
func TestProxyCardKeepsBracketsInName(t *testing.T) {
	node := mihomotui.ProxyNode{Name: "香港[HK] 01", Type: "Shadowsocks", Delay: 50}
	card := createProxyCard(node, false, 30, func() {}, func() {})
	lines := renderPrimitive(t, card, 36, 4)
	if !strings.Contains(lines[1], "香港[HK] 01") {
		t.Fatalf("方括号被吞掉: %q", lines[1])
	}
}

// 选中态同样处理（另一套文本模板，避免只修了未选中分支）。
func TestProxyCardSelectedBranchSanitized(t *testing.T) {
	node := mihomotui.ProxyNode{Name: "🇸🇬AWS新加坡02 | 电信移动联通推荐", Type: "Trojan", Delay: 88}
	card := createProxyCard(node, true, 20, func() {}, func() {})
	lines := renderPrimitive(t, card, 30, 4)
	if strings.ContainsRune(strings.Join(lines, ""), '🇸') {
		t.Fatalf("选中态仍在输出区域指示符: %q", lines)
	}
	if !strings.Contains(lines[1], "SGAWS") {
		t.Fatalf("选中态国旗未转换: %q", lines[1])
	}
	if !strings.Contains(lines[2], "Trojan") || !strings.Contains(lines[2], "88ms") {
		t.Fatalf("选中态类型/延迟行丢失: %q", lines)
	}
}

// nameWidth 为 0（宽度未知）时不应截断，也不应 panic。
func TestProxyCardWithoutKnownWidth(t *testing.T) {
	node := mihomotui.ProxyNode{Name: "🇯🇵AWS日本06", Type: "Shadowsocks", Delay: 1}
	card := createProxyCard(node, false, 0, func() {}, func() {})
	lines := renderPrimitive(t, card, 40, 4)
	if !strings.Contains(lines[1], "JPAWS日本06") {
		t.Fatalf("宽度未知时不应截断: %q", lines[1])
	}
}

// 卡片较窄时必须保住延迟信息：类型可以被截断，延迟不能被裁剪或挤到卡片外。
func TestProxyCardKeepsDelayVisibleWhenNarrow(t *testing.T) {
	delays := []struct {
		delay int
		text  string
	}{
		{88, "88ms"},
		{-1, "超时"},
		{-3, "未测试"},
		{-2, "测试中"},
		{1234, "1234ms"},
	}
	types := []string{"Shadowsocks", "Vmess", "Trojan", "WireGuard"}
	for _, d := range delays {
		for _, typ := range types {
			node := mihomotui.ProxyNode{Name: "🇯🇵AWS日本06 | 电信移动联通推荐", Type: typ, Delay: d.delay}
			card := createProxyCard(node, false, 16, func() {}, func() {})
			lines := renderPrimitive(t, card, 26, 4)
			if !strings.Contains(lines[2], d.text) {
				t.Fatalf("type=%s delay=%s 的延迟被裁剪: %q", typ, d.text, lines[2])
			}
			if width := uniseg.StringWidth(lines[2]); width > 26 {
				t.Fatalf("第二行宽度 %d 超出卡片宽度: %q", width, lines[2])
			}
		}
	}
}
