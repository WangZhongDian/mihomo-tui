package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

func formatSize(kb float64) string {
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.2f GB", kb/1024/1024)
	}
	if kb >= 1024 {
		return fmt.Sprintf("%.2f MB", kb/1024)
	}
	return fmt.Sprintf("%.2f KB", kb)
}

func formatSpeed(bps float64) string {
	if bps >= 1024*1024 {
		return fmt.Sprintf("%.2f MB/s", bps/1024/1024)
	}
	if bps >= 1024 {
		return fmt.Sprintf("%.2f KB/s", bps/1024)
	}
	return fmt.Sprintf("%.2f B/s", bps)
}

// NewConnectionsPage 创建连接页面
func NewConnectionsPage(app *tview.Application) tview.Primitive {
	connections := []mihomotui.Connection{}

	activeTab := true // true=活跃, false=已关闭
	filterKeyword := ""

	// 统计信息
	statsText := tview.NewTextView().
		SetDynamicColors(true).
		SetText("")

	// 关闭全部按钮
	closeAllBtn := tview.NewButton(" 关闭全部 ")
	closeAllBtn.SetBorder(false)
	closeAllBtn.SetSelectedFunc(func() {
		go func() {
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				return
			}
			_ = api.CloseAllConnections()
		}()
	})

	// 第一行工具栏：统计 + 关闭全部
	topBar := tview.NewFlex().
		AddItem(statsText, 0, 1, false).
		AddItem(closeAllBtn, 12, 0, true)

	// Tab 切换：活跃 / 已关闭
	activeBtn := tview.NewButton(" 活跃 ")
	activeBtn.SetBorder(false)
	closedBtn := tview.NewButton(" 已关闭 ")
	closedBtn.SetBorder(false)

	// 过滤输入框
	inputField := tview.NewInputField().
		SetPlaceholder(" 过滤条件").
		SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	inputField.SetBorder(true)

	// 第二行工具栏
	midBar := tview.NewFlex().
		AddItem(activeBtn, 8, 0, true).
		AddItem(closedBtn, 10, 0, true).
		AddItem(inputField, 0, 1, true)

	// 连接表格
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetSeparator(' ')

	// 更新统计信息
	updateStats := func() {
		totalDown, totalUp := 0.0, 0.0
		activeCount := 0
		for _, c := range connections {
			if c.Active {
				activeCount++
				totalDown += c.Download
				totalUp += c.Upload
			}
		}
		statsText.SetText(fmt.Sprintf(
			" ['+mihomotui.ColorWarn+']●[-] %d 活跃  |  下载: %s  |  上传: %s",
			activeCount,
			formatSize(totalDown),
			formatSize(totalUp),
		))
	}

	// 刷新表格
	refreshTable := func() {}
	refreshTable = func() {
		table.Clear()

		// 表头
		headers := []string{"主机", "下载", "上传", "下载速度", "上传速度", "链路"}
		for i, h := range headers {
			cell := tview.NewTableCell("[::b] " + h + " ").
				SetTextColor(tview.Styles.SecondaryTextColor).
				SetSelectable(false)
			table.SetCell(0, i, cell)
		}

		row := 1
		for _, c := range connections {
			if activeTab && !c.Active {
				continue
			}
			if !activeTab && c.Active {
				continue
			}
			if filterKeyword != "" && !strings.Contains(c.Host, filterKeyword) {
				continue
			}

			activeMarker := "['+mihomotui.ColorOK+']●[-] "
			if !c.Active {
				activeMarker = "['+mihomotui.ColorMuted+']○[-] "
			}

			table.SetCell(row, 0, tview.NewTableCell(" "+activeMarker+c.Host))
			table.SetCell(row, 1, tview.NewTableCell(" "+formatSize(c.Download)))
			table.SetCell(row, 2, tview.NewTableCell(" "+formatSize(c.Upload)))
			table.SetCell(row, 3, tview.NewTableCell(" "+formatSpeed(c.DownSpeed)))
			table.SetCell(row, 4, tview.NewTableCell(" "+formatSpeed(c.UpSpeed)))
			table.SetCell(row, 5, tview.NewTableCell(" "+c.Chain))
			row++
		}

		updateStats()
	}

	// Tab 切换
	activeBtn.SetSelectedFunc(func() {
		activeTab = true
		refreshTable()
	})
	closedBtn.SetSelectedFunc(func() {
		activeTab = false
		refreshTable()
	})

	// 过滤
	inputField.SetChangedFunc(func(text string) {
		filterKeyword = text
		refreshTable()
	})

	// 速度计算状态
	var prevConns []mihomotui.Connection
	var prevTime time.Time

	refreshWithSpeed := func(newConns []mihomotui.Connection) {
		now := time.Now()
		if !prevTime.IsZero() && len(prevConns) > 0 {
			elapsed := now.Sub(prevTime).Seconds()
			if elapsed > 0 {
				prevMap := make(map[string]mihomotui.Connection)
				for _, c := range prevConns {
					prevMap[c.ID] = c
				}
				for i := range newConns {
					if prev, ok := prevMap[newConns[i].ID]; ok {
						// Download/Upload 单位是 KB，差值 * 1024 = B，除以秒 = B/s
						newConns[i].DownSpeed = (newConns[i].Download - prev.Download) * 1024 / elapsed
						newConns[i].UpSpeed = (newConns[i].Upload - prev.Upload) * 1024 / elapsed
						if newConns[i].DownSpeed < 0 {
							newConns[i].DownSpeed = 0
						}
						if newConns[i].UpSpeed < 0 {
							newConns[i].UpSpeed = 0
						}
					}
				}
			}
		}
		prevConns = make([]mihomotui.Connection, len(newConns))
		copy(prevConns, newConns)
		prevTime = now
		connections = newConns
		refreshTable()
	}

	// 定时从 IPC 获取真实连接数据
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			api, err := mihomotui.GetMihomoAPI()
			if err != nil {
				continue
			}
			conns, err := api.GetConnectionsParsed()
			if err != nil {
				continue
			}
			app.QueueUpdateDraw(func() {
				refreshWithSpeed(conns)
			})
		}
	}()

	refreshTable()

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(midBar, 3, 0, true).
		AddItem(table, 0, 1, true)

	return content
}
