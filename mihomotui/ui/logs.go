package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// LogEntry 单条日志
type LogEntry struct {
	Time  string
	Level string
	Msg   string
}

const maxLogBuffer = 2000

// batchLogEntry 批量日志缓冲条目
type batchLogEntry struct {
	entries []LogEntry
}

// NewLogsPage 创建日志页面
func NewLogsPage(app *tview.Application) tview.Primitive {
	logs := make([]LogEntry, 0, maxLogBuffer)
	currentLevel := "" // "" 表示全部

	// 日志级别下拉框
	levelDropdown := tview.NewDropDown().
		SetLabel(" 级别: ").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	levelDropdown.AddOption("全部", nil)
	levelDropdown.AddOption("DEBUG", nil)
	levelDropdown.AddOption("INFO", nil)
	levelDropdown.AddOption("WARNING", nil)
	levelDropdown.AddOption("ERROR", nil)
	levelDropdown.SetCurrentOption(0)

	// 清空按钮
	clearBtn := tview.NewButton(" 清空 ")
	clearBtn.SetBorder(false)

	// 顶部工具栏
	toolbar := tview.NewFlex().
		AddItem(levelDropdown, 0, 1, true).
		AddItem(clearBtn, 10, 0, true)

	// 日志显示区域：关闭自动重绘，由批量定时器统一控制
	logView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true)

	// 单条日志格式化为字符串
	formatLogLine := func(entry LogEntry) string {
		color := "white"
		switch entry.Level {
		case "DEBUG":
			color = "gray"
		case "INFO":
			color = "green"
		case "WARNING":
			color = "yellow"
		case "ERROR":
			color = "red"
		}
		return fmt.Sprintf("[%s]%s [%-7s] %s[-]\n", color, entry.Time, entry.Level, entry.Msg)
	}

	// 批量追加到视图：用 strings.Builder 一次性写入，减少 TextView 内部重排
	batchAppendToView := func(entries []LogEntry) {
		var b strings.Builder
		for _, entry := range entries {
			if currentLevel != "" && entry.Level != currentLevel {
				continue
			}
			b.WriteString(formatLogLine(entry))
		}
		if b.Len() > 0 {
			fmt.Fprint(logView, b.String())
		}
	}

	// 重新渲染全部日志（仅在切换级别时使用）
	renderAllLogs := func() {
		logView.Clear()
		var b strings.Builder
		for _, entry := range logs {
			if currentLevel != "" && entry.Level != currentLevel {
				continue
			}
			b.WriteString(formatLogLine(entry))
		}
		if b.Len() > 0 {
			fmt.Fprint(logView, b.String())
		}
	}

	// 级别切换
	levelDropdown.SetSelectedFunc(func(text string, index int) {
		if text == "全部" {
			currentLevel = ""
		} else {
			currentLevel = text
		}
		renderAllLogs()
	})

	// 清空
	clearBtn.SetSelectedFunc(func() {
		logs = logs[:0]
		logView.Clear()
	})

	// 批量日志缓冲 channel + 定时刷新
	logBatchCh := make(chan []LogEntry, 16)

	// 消费者：定时批量刷新到 UI
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		var pending []LogEntry
		for {
			select {
			case batch, ok := <-logBatchCh:
				if !ok {
					// channel 关闭，刷完剩余退出
					if len(pending) > 0 {
						app.QueueUpdateDraw(func() {
							batchAppendToView(pending)
						})
					}
					return
				}
				pending = append(pending, batch...)

			case <-ticker.C:
				if len(pending) == 0 {
					continue
				}
				// 复制一份 pending，清空原切片，然后在 UI 线程写入
				toFlush := make([]LogEntry, len(pending))
				copy(toFlush, pending)
				pending = pending[:0]

				app.QueueUpdateDraw(func() {
					// 同步更新内存日志缓存
					for _, entry := range toFlush {
						if len(logs) >= maxLogBuffer {
							logs = logs[1:]
						}
						logs = append(logs, entry)
					}
					batchAppendToView(toFlush)
				})
			}
		}
	}()

	// 获取 mihomo 实时日志流
	go func() {
		api, err := mihomotui.GetMihomoAPI()
		if err != nil {
			mihomotui.Warnf("获取日志流失败: %v", err)
			return
		}
		resp, err := api.GetLogsStream("")
		if err != nil {
			mihomotui.Warnf("获取日志流失败: %v", err)
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		var batch []LogEntry
		const batchSize = 100

		flushBatch := func() {
			if len(batch) == 0 {
				return
			}
			// 复制后发送到 channel，避免被后续修改覆盖
			toSend := make([]LogEntry, len(batch))
			copy(toSend, batch)
			select {
			case logBatchCh <- toSend:
			default:
				// channel 满则丢弃最老的一批，保证不阻塞 SSE 读取线程
				select {
				case <-logBatchCh:
				default:
				}
				select {
				case logBatchCh <- toSend:
				default:
				}
			}
			batch = batch[:0]
		}

		for scanner.Scan() {
			line := scanner.Text()
			// mihomo /logs 返回 SSE 格式: data: {"type":"info","payload":"xxx"}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				line = strings.TrimSpace(after)
			}
			if line == "" {
				continue
			}
			var logMsg struct {
				Type    string `json:"type"`
				Payload string `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &logMsg); err != nil {
				continue
			}
			level := "INFO"
			switch logMsg.Type {
			case "debug":
				level = "DEBUG"
			case "warning":
				level = "WARNING"
			case "error":
				level = "ERROR"
			}
			batch = append(batch, LogEntry{
				Time:  time.Now().Format("15:04:05"),
				Level: level,
				Msg:   logMsg.Payload,
			})

			if len(batch) >= batchSize {
				flushBatch()
			}
		}
		// 刷完最后一批
		flushBatch()
	}()

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(toolbar, 3, 0, true).
		AddItem(logView, 0, 1, true)

	return page
}
