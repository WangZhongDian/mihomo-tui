package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
)

// renderText 用 tcell 的模拟屏幕渲染一段动态颜色文本，返回每一行的实际内容。
// 用于验证"文字被标签吞掉""宽字符占位错误"这类只在渲染阶段才暴露的问题。
func renderText(t *testing.T, text string, width, height int) []string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	tv := tview.NewTextView().SetDynamicColors(true).SetText(text)
	tv.SetRect(0, 0, width, height)
	tv.Draw(screen)
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
			// 宽字符（中文/emoji）在模拟屏幕里会额外占用一个填充格，
			// 跳过它，避免重建出的文本被插入多余空格。
			x += uniseg.StringWidth(content) - 1
		}
		lines = append(lines, strings.TrimRight(sb.String(), " "))
	}
	return lines
}

// 国旗 emoji 被替换成字母：宽度固定，且不再依赖终端字体是否支持国旗连字。
func TestSanitizeDisplayTextReplacesFlagEmoji(t *testing.T) {
	cases := map[string]string{
		"🇯🇵AWS日本06 | 电信移动联通推荐": "JPAWS日本06 | 电信移动联通推荐",
		"🇸🇬AWS新加坡02":           "SGAWS新加坡02",
		"🇭🇰 香港01":              "HK 香港01",
		"🇯🇵":                   "JP",
	}
	for in, want := range cases {
		got := SanitizeDisplayText(in)
		if got != want {
			t.Fatalf("SanitizeDisplayText(%q) = %q, want %q", in, got, want)
		}
		for _, r := range got {
			if r >= regionalIndicatorFirst && r <= regionalIndicatorLast {
				t.Fatalf("输出仍含区域指示符: %q", got)
			}
		}
	}
}

// ZWJ 组合、变体选择符、键帽都会让终端自行决定渲染宽度，统一收敛为单字符。
func TestSanitizeDisplayTextCollapsesAmbiguousClusters(t *testing.T) {
	cases := map[string]string{
		"🏳️‍🌈 彩虹节点": "🏳 彩虹节点",
		"✈️ 航班":     "✈ 航班",
		"1️⃣ 一号":    "1 一号",
	}
	for in, want := range cases {
		if got := SanitizeDisplayText(in); got != want {
			t.Fatalf("SanitizeDisplayText(%q) = %q, want %q", in, got, want)
		}
	}
}

// 节点名里的方括号必须原样显示，不能被 tview 当成颜色/区域标签吞掉。
func TestSanitizeDisplayTextEscapesTviewTags(t *testing.T) {
	raw := "香港[HK] 01"
	rendered := renderText(t, " ● "+SanitizeDisplayText(raw), 40, 2)
	if got := strings.TrimSpace(rendered[0]); got != "● 香港[HK] 01" {
		t.Fatalf("方括号名称渲染错误: %q", got)
	}

	// 反证：未经转义时 tview 会吞掉 [HK]。
	unescaped := renderText(t, " ● 香港[HK] 01", 40, 2)
	if strings.TrimSpace(unescaped[0]) != "● 香港 01" {
		t.Skipf("tview 标签解析行为已变化，本反证不再适用: %q", unescaped[0])
	}
}

// 截断按显示宽度计算，不能切开中文或 emoji。
func TestTruncateDisplayRespectsWidth(t *testing.T) {
	cases := []struct {
		in    string
		max   int
		check func(string) bool
	}{
		{"short", 20, func(s string) bool { return s == "short" }},
		{"电信移动联通推荐", 7, func(s string) bool { return uniseg.StringWidth(s) <= 7 && strings.HasSuffix(s, "…") }},
		{"🇯🇵AWS日本06 | 电信移动联通推荐", 12, func(s string) bool { return uniseg.StringWidth(s) <= 12 && strings.HasSuffix(s, "…") }},
		{"任意文本", 1, func(s string) bool { return s == "…" }},
		{"任意文本", 0, func(s string) bool { return s == "" }},
	}
	for _, c := range cases {
		got := TruncateDisplay(c.in, c.max)
		if !c.check(got) {
			t.Fatalf("TruncateDisplay(%q, %d) = %q", c.in, c.max, got)
		}
	}
}

// DisplayName 组合入口：先归一化宽度再截断，宽度恒不超过上限。
func TestDisplayNameCombinesSanitizeAndTruncate(t *testing.T) {
	got := DisplayName("🇯🇵AWS日本06 | 电信移动联通推荐", 10)
	if strings.ContainsRune(got, '🇯') {
		t.Fatalf("未归一化国旗: %q", got)
	}
	if width := uniseg.StringWidth(got); width > 10 {
		t.Fatalf("宽度 %d 超出上限 10: %q", width, got)
	}
}

// 宽度未知（<=0）时只归一化、不截断，避免调用方拿不到宽度就把名称清空。
func TestDisplayNameWithoutWidthDoesNotTruncate(t *testing.T) {
	in := "🇯🇵AWS日本06 | 电信移动联通推荐"
	got := DisplayName(in, 0)
	if got != "JPAWS日本06 | 电信移动联通推荐" {
		t.Fatalf("DisplayName(%q, 0) = %q", in, got)
	}
}

// 截断必须先于标签转义：否则会切断 `[HK[]` 这类转义序列，把方括号语法漏到界面上。
func TestDisplayNameTruncatesBeforeEscaping(t *testing.T) {
	got := DisplayName("香港[HK]节点名称很长很长", 8)
	rendered := renderText(t, got, 30, 2)
	line := strings.TrimRight(rendered[0], " ")
	if strings.Contains(line, "[HK[") || strings.Contains(line, "[]") {
		t.Fatalf("截断破坏了转义序列: %q", line)
	}
	if !strings.HasSuffix(line, "…") {
		t.Fatalf("超长名称应被截断: %q", line)
	}
	if width := uniseg.StringWidth(line); width > 8 {
		t.Fatalf("显示宽度 %d 超过上限 8: %q", width, line)
	}
}
