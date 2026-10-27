package ui

import (
	"io"
	"os"

	"github.com/gdamore/tcell/v2/terminfo"
)

// 退出时用于还原终端的转义序列。
//
// tcell 的 Fini() 会完成同样的工作，但它只有在被调用时才生效；
// 进程被信号杀死、或者 Fini 之后的兜底路径没有走到时，终端会被留在
// 「alt screen + 鼠标上报 + 隐藏光标 + raw 模式」的状态里。
// 此时终端会不断把鼠标事件发回来，shell/readline 把 SGR 鼠标序列当成普通
// 文本回显，于是操作滚轮/按键越多，屏幕上残留的垃圾行就越多。
//
// 下列序列都是幂等的：已经还原过的状态再执行一次不会有副作用。
const (
	escResetCursorStyle = "\x1b[0 q"
	escExitAltScreen    = "\x1b[?1049l"
	escCloseMouseX10    = "\x1b[?9l"
	escCloseMouse1000   = "\x1b[?1000l"
	escCloseMouse1002   = "\x1b[?1002l"
	escCloseMouse1003   = "\x1b[?1003l"
	escCloseMouse1006   = "\x1b[?1006l"
	escClosePaste       = "\x1b[?2004l"
	escCloseFocus       = "\x1b[?1004l"
	escExitKeypad       = "\x1b>"
	escResetAttrs       = "\x1b[0m"
	escShowCursor       = "\x1b[?25h"
	escClearScreen      = "\x1b[2J\x1b[H"
)

// terminalRestoreSequence 返回退出时应写入终端的还原序列。
// clearScreen 为 true 时额外清屏——仅用于不支持备用屏幕的终端
// （例如 Linux 控制台的 TERM=linux），否则会把用户原来的画面擦掉。
func terminalRestoreSequence(clearScreen bool) string {
	seq := escResetCursorStyle +
		escExitAltScreen +
		escCloseMouse1000 + escCloseMouse1002 + escCloseMouse1003 +
		escCloseMouse1006 + escCloseMouseX10 +
		escClosePaste + escCloseFocus +
		escExitKeypad + escResetAttrs + escShowCursor
	if clearScreen {
		seq += escClearScreen
	}
	return seq
}

// terminalSupportsAltScreen 报告当前 TERM 是否支持备用屏幕。
//
// 不支持时（典型是 Linux 控制台的 TERM=linux，smcup 为空），tcell 不会进入
// 备用屏幕，所有重绘都会落在正常屏幕上并且留在滚动缓冲里。这种情况下退出必须
// 主动清屏，否则用户会看到一整屏残留的 TUI 画面。
func terminalSupportsAltScreen() bool {
	if os.Getenv("TCELL_ALTSCREEN") == "disable" {
		return false
	}
	term := os.Getenv("TERM")
	if term == "" {
		return false
	}
	ti, err := terminfo.LookupTerminfo(term)
	if err != nil || ti == nil {
		return false
	}
	return ti.EnterCA != ""
}

// restoreTerminal 在 TUI 退出后把终端恢复到可用状态。
// 只写转义序列，不输出任何可见文本；stdout 不是终端时（被重定向到文件/管道）
// 直接跳过，避免把转义序列写进日志。
func restoreTerminal(w io.Writer) {
	if w == nil {
		return
	}
	if f, ok := w.(*os.File); ok {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return
		}
	}
	_, _ = io.WriteString(w, terminalRestoreSequence(!terminalSupportsAltScreen()))
}

// terminalRestore 是 restoreTerminal 面向 os.Stdout 的便捷封装。
func terminalRestore() { restoreTerminal(os.Stdout) }
