package ui

import (
	"fmt"
	"strings"

	"mihomotui/mihomotui"
)

// 延迟分档阈值（毫秒）。分档含义：
//
//	delay <= 200       绿色（快）
//	200 < delay <= 400 蓝色（正常）
//	400 < delay <= 600 黄色（偏慢）
//	delay > 600        红色（慢）
//
// 边界归入较快的档位，避免 200ms 这种整数边界出现"刚过 200 就变蓝"的跳变歧义。
const (
	DelayFastThresholdMs    = 200
	DelayNormalThresholdMs  = 400
	DelaySlowThresholdMs    = 600
	delayTimeoutDisplayText = "timeout"
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
	case delay < 0:
		return mihomotui.ColorMuted
	case delay <= DelayFastThresholdMs:
		return mihomotui.ColorOK
	case delay <= DelayNormalThresholdMs:
		return mihomotui.ColorInfo
	case delay <= DelaySlowThresholdMs:
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
		return delayTimeoutDisplayText
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
