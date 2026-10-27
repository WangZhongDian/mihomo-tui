package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// 还原序列必须关闭所有会让终端持续输出的开关：
// 备用屏幕、各鼠标上报模式、粘贴/焦点上报、键盘应用模式、隐藏光标。
func TestTerminalRestoreSequenceDisablesAllModes(t *testing.T) {
	seq := terminalRestoreSequence(false)
	for _, want := range []struct {
		name string
		esc  string
	}{
		{"退出备用屏幕", escExitAltScreen},
		{"关闭1000鼠标", escCloseMouse1000},
		{"关闭1002鼠标", escCloseMouse1002},
		{"关闭1003鼠标", escCloseMouse1003},
		{"关闭1006鼠标(SGR)", escCloseMouse1006},
		{"关闭粘贴", escClosePaste},
		{"关闭焦点上报", escCloseFocus},
		{"退出键盘应用模式", escExitKeypad},
		{"重置属性", escResetAttrs},
		{"显示光标", escShowCursor},
	} {
		if !strings.Contains(seq, want.esc) {
			t.Fatalf("还原序列缺少「%s」(%q)", want.name, want.esc)
		}
	}
	// 默认不清屏：备用屏幕退出后应保留用户原有画面。
	if strings.Contains(seq, escClearScreen) {
		t.Fatal("支持备用屏幕时不应清屏，否则会擦掉用户原有画面")
	}
}

// 不支持备用屏幕的终端（TERM=linux）必须清屏，否则整屏 TUI 画面会残留。
func TestTerminalRestoreSequenceClearsWithoutAltScreen(t *testing.T) {
	seq := terminalRestoreSequence(true)
	if !strings.Contains(seq, escClearScreen) {
		t.Fatal("无备用屏幕时必须清屏")
	}
	if !strings.Contains(seq, escShowCursor) {
		t.Fatal("必须恢复光标显示")
	}
}

// 还原序列里不能夹带任何可见文本——它会被直接写进终端。
func TestTerminalRestoreSequenceHasNoText(t *testing.T) {
	seq := terminalRestoreSequence(true)
	// 去掉所有 CSI / OSC 序列后不应剩下任何字符。
	stripped := stripEscapeSequences(seq)
	if stripped != "" {
		t.Fatalf("还原序列含有可见文本: %q", stripped)
	}
}

// stripEscapeSequences 移除 CSI(ESC[...) 与双字符 ESC 序列，用于断言。
func stripEscapeSequences(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			out.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		if s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
				j++
			}
			i = j
			continue
		}
		i++ // ESC + 单字符（如 ESC>）
	}
	return out.String()
}

// TERM 不支持备用屏幕时（Linux 控制台）应判定为需要清屏。
func TestTerminalSupportsAltScreen(t *testing.T) {
	t.Setenv("TCELL_ALTSCREEN", "")
	t.Setenv("TERM", "linux")
	if terminalSupportsAltScreen() {
		t.Fatal("TERM=linux 没有 smcup，不应判定为支持备用屏幕")
	}

	t.Setenv("TERM", "xterm-256color")
	if !terminalSupportsAltScreen() {
		t.Fatal("TERM=xterm-256color 应支持备用屏幕")
	}

	// 用户显式禁用备用屏幕时以环境变量为准。
	t.Setenv("TCELL_ALTSCREEN", "disable")
	if terminalSupportsAltScreen() {
		t.Fatal("TCELL_ALTSCREEN=disable 时应判定为不支持")
	}

	t.Setenv("TCELL_ALTSCREEN", "")
	t.Setenv("TERM", "")
	if terminalSupportsAltScreen() {
		t.Fatal("TERM 为空时应判定为不支持")
	}
}

// stdout 被重定向到文件/管道时不得写入转义序列。
func TestRestoreTerminalSkipsNonTTY(t *testing.T) {
	var buf bytes.Buffer
	f, err := os.CreateTemp(t.TempDir(), "out-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// *os.File 但不是字符设备 → 跳过。
	restoreTerminal(f)
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("重定向到普通文件时不应写入转义序列，实际写入 %d 字节", info.Size())
	}

	// 普通 io.Writer（非 *os.File）保持可写，便于测试与未来替换。
	restoreTerminal(&buf)
	if !strings.Contains(buf.String(), escExitAltScreen) {
		t.Fatal("普通 writer 应收到还原序列")
	}

	// nil writer 不应 panic。
	restoreTerminal(nil)
}
