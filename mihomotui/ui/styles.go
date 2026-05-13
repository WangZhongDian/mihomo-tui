package ui

import (
	"fmt"
	"strings"

	"mihomotui/mihomotui"
)

// DelayColor 根据延迟值返回对应的颜色标签
func DelayColor(delay int) string {
	switch {
	case delay == mihomotui.DelayUntested:
		return mihomotui.ColorMuted
	case delay == mihomotui.DelayTesting:
		return mihomotui.ColorWarn
	case delay == mihomotui.DelayTimeout:
		return mihomotui.ColorError
	case delay < 100:
		return mihomotui.ColorOK
	case delay < 200:
		return mihomotui.ColorWarn
	default:
		return mihomotui.ColorError
	}
}

// DelayText 根据延迟值返回对应的显示文本
func DelayText(delay int) string {
	switch delay {
	case mihomotui.DelayUntested:
		return "未测试"
	case mihomotui.DelayTesting:
		return "测试中"
	case mihomotui.DelayTimeout:
		return "超时"
	default:
		if delay >= 0 {
			return fmt.Sprintf("%dms", delay)
		}
		return "未知"
	}
}

// ProgressBar 生成进度条字符串
func ProgressBar(width int, percent float64) string {
	filled := 0
	if percent > 0 {
		filled = min(int(percent/100*float64(width)), width)
	}
	return fmt.Sprintf("[%s]%s[-]%s", mihomotui.ColorInfo,
		strings.Repeat("━", filled),
		strings.Repeat("─", width-filled))
}
