package ui

import (
	"strings"

	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
)

// keycap combining enclosing keycap
const keycapCombining rune = 0x20E3

// 区域指示符号（Regional Indicator Symbol）：U+1F1E6..U+1F1FF 对应 A..Z。
// 终端把一对指示符渲染成国旗 emoji 时，显示宽度依赖字体：有的渲染成 2 格国旗，
// 有的渲染成两个各占 2 格的方框字母（如 "JP" 各带边框）。tview/uniseg 固定按 2 格
// 排布，遇到后一种字体时后续文字会压到第二个字母上，表现为"开头的字体重叠"。
const (
	regionalIndicatorFirst rune = 0x1F1E6
	regionalIndicatorLast  rune = 0x1F1FF
	letterA                rune = 'A'
)

// SanitizeDisplayText 把订阅/节点/策略组名称转换成终端宽度确定、且不会被 tview
// 误解析成样式标签的展示文本。它只影响显示，绝不用于任何 API 调用或比较。
//
// 处理三类会破坏布局的字符序列：
//  1. 区域指示符（国旗 emoji）：替换为对应的 ASCII 国家/地区字母，宽度恒为 2。
//  2. ZWJ 组合、变体选择符、键帽序列：缩减为单个基字符，避免同一个字形在
//     不同终端上占 1/2/4 格不等。
//  3. tview 样式标签（如 [JP]、[HK]）：转义为字面量，否则 tview 会把它们
//     当成颜色/区域标签吞掉，标题文字凭空少一段。
func SanitizeDisplayText(text string) string {
	if text == "" {
		return ""
	}
	cleaned := collapseAmbiguousClusters(text)
	// Escape 之后 tview 会把 [xxx] 原样画出来。
	return tview.Escape(cleaned)
}

// collapseAmbiguousClusters 归一化宽度不确定的字符簇（不含标签转义）。
func collapseAmbiguousClusters(text string) string {
	if !needsClusterNormalization(text) {
		return text
	}
	var sb strings.Builder
	sb.Grow(len(text))
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		cluster := g.Str()
		runes := []rune(cluster)
		if code, ok := regionalIndicatorCode(runes); ok {
			sb.WriteString(code)
			continue
		}
		if hasAmbiguousWidthComponent(runes) {
			// 只保留第一个基字符：ZWJ/变体选择符/键帽都会让终端自行决定宽度。
			base := runes[0]
			if base != keycapCombining {
				sb.WriteRune(base)
			}
			continue
		}
		sb.WriteString(cluster)
	}
	return sb.String()
}

// needsClusterNormalization 快速判断字符串是否含有需要归一化的码点。
func needsClusterNormalization(text string) bool {
	for _, r := range text {
		switch {
		case r >= regionalIndicatorFirst && r <= regionalIndicatorLast:
			return true
		case r == 0xFE0E || r == 0xFE0F: // variation selectors
			return true
		case r == 0x200D: // zero-width joiner
			return true
		case r == keycapCombining:
			return true
		}
	}
	return false
}

// regionalIndicatorCode 把一到两个区域指示符转换成对应的 ASCII 字母（如 🇯🇵 → JP）。
// 单个指示符也转换，避免剩下的半对在终端上被渲染成孤立方框。
func regionalIndicatorCode(runes []rune) (string, bool) {
	if len(runes) == 0 || len(runes) > 2 {
		return "", false
	}
	var sb strings.Builder
	for _, r := range runes {
		if r < regionalIndicatorFirst || r > regionalIndicatorLast {
			return "", false
		}
		sb.WriteRune(letterA + (r - regionalIndicatorFirst))
	}
	return sb.String(), true
}

// hasAmbiguousWidthComponent 报告字符簇是否含有会让终端自行决定宽度的成分。
func hasAmbiguousWidthComponent(runes []rune) bool {
	for _, r := range runes[1:] {
		if r == 0xFE0E || r == 0xFE0F || r == 0x200D || r == keycapCombining {
			return true
		}
	}
	return false
}

// TruncateDisplay 按终端显示宽度截断文本（以字素簇为最小单位，不会切开 emoji
// 或中文），超长时以省略号结尾。用于卡片这类固定高度区域，避免长名称换行后
// 把同卡片里的类型/延迟整行挤掉。
func TruncateDisplay(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if uniseg.StringWidth(text) <= maxWidth {
		return text
	}
	if maxWidth == 1 {
		return "…"
	}
	limit := maxWidth - 1 // 预留 1 格给省略号
	var sb strings.Builder
	width := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		clusterWidth := g.Width()
		if width+clusterWidth > limit {
			break
		}
		sb.WriteString(g.Str())
		width += clusterWidth
	}
	if sb.Len() == 0 {
		return "…"
	}
	return sb.String() + "…"
}

// DisplayName 是"名称类"展示的推荐入口：先归一化宽度、再按可用宽度截断，
// 最后才转义 tview 标签。顺序很重要——若先转义再截断，可能把 `[HK[]` 这类
// 转义序列切断，反而把标签语法暴露到界面上。
// maxWidth <= 0 表示宽度未知，只做归一化不截断。
func DisplayName(name string, maxWidth int) string {
	cleaned := collapseAmbiguousClusters(name)
	if maxWidth > 0 {
		cleaned = TruncateDisplay(cleaned, maxWidth)
	}
	return tview.Escape(cleaned)
}
