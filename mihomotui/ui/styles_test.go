package ui

import (
	"testing"

	"mihomotui/mihomotui"
)

// 延迟分档：<=200 绿、<=400 蓝、<=600 黄、>600 红；超时用 timeout 文案。
func TestDelayColorThresholds(t *testing.T) {
	cases := []struct {
		delay int
		want  string
	}{
		{0, mihomotui.ColorOK},
		{1, mihomotui.ColorOK},
		{199, mihomotui.ColorOK},
		{200, mihomotui.ColorOK},   // 边界归入"以内"
		{201, mihomotui.ColorInfo}, // 超过 200 变蓝
		{399, mihomotui.ColorInfo},
		{400, mihomotui.ColorInfo}, // 边界归入较快档
		{401, mihomotui.ColorWarn}, // 超过 400 变黄
		{599, mihomotui.ColorWarn},
		{600, mihomotui.ColorWarn},  // 边界归入较快档
		{601, mihomotui.ColorError}, // 超过 600 变红
		{9999, mihomotui.ColorError},

		// 非数值状态不能被阈值吞掉。
		{mihomotui.DelayUntested, mihomotui.ColorMuted},
		{mihomotui.DelayTesting, mihomotui.ColorWarn},
		{mihomotui.DelayTimeout, mihomotui.ColorError},
	}
	for _, c := range cases {
		if got := DelayColor(c.delay); got != c.want {
			t.Fatalf("DelayColor(%d) = %q, want %q", c.delay, got, c.want)
		}
	}
}

// 每个延迟档位都必须有可见的颜色差异（不能两档共用同一颜色）。
func TestDelayColorDistinctPerBand(t *testing.T) {
	bands := map[string]int{
		"fast":   150,
		"normal": 300,
		"slow":   500,
		"bad":    800,
	}
	seen := map[string]string{}
	for name, delay := range bands {
		color := DelayColor(delay)
		if prev, dup := seen[color]; dup {
			t.Fatalf("档位 %s 与 %s 使用相同颜色 %q", name, prev, color)
		}
		seen[color] = name
	}
	if len(seen) != len(bands) {
		t.Fatalf("期望 4 种不同颜色，实际 %d 种: %+v", len(seen), seen)
	}
}

// 超时显示为 timeout；其余状态与数值文案保持不变。
func TestDelayText(t *testing.T) {
	useTestLanguage(t, "zh-CN")
	cases := []struct {
		delay int
		want  string
	}{
		{mihomotui.DelayTimeout, "timeout"},
		{mihomotui.DelayUntested, "未测试"},
		{mihomotui.DelayTesting, "测试中"},
		{0, "0ms"},
		{200, "200ms"},
		{1234, "1234ms"},
	}
	for _, c := range cases {
		if got := DelayText(c.delay); got != c.want {
			t.Fatalf("DelayText(%d) = %q, want %q", c.delay, got, c.want)
		}
	}
}
