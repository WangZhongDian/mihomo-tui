package ui

import (
	"fmt"
	"time"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// NewResourcePage is the dedicated management page for downloadable runtime
// resources. Settings intentionally contains configuration only.
func NewResourcePage(app *tview.Application) tview.Primitive {
	pages := tview.NewPages()
	var show func(string)

	kernel := newKernelResourceView(app)
	external := newExternalResourceView(app)
	pages.AddPage("kernel", kernel, true, true)
	pages.AddPage("external", external, true, false)
	kernelBtn := tview.NewButton(" 内核管理 ").SetSelectedFunc(func() { show("kernel") })
	externalBtn := tview.NewButton(" 外部资源 ").SetSelectedFunc(func() { show("external") })
	show = func(name string) { pages.SwitchToPage(name) }
	tabs := tview.NewFlex().AddItem(kernelBtn, 0, 1, true).AddItem(externalBtn, 0, 1, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(tabs, 1, 0, true).AddItem(pages, 0, 1, true)
}

func newKernelResourceView(app *tview.Application) tview.Primitive {
	modalHost := tview.NewPages()
	table := tview.NewTable().SetSelectable(true, true).SetFixed(1, 0)
	table.SetBorder(true).SetTitle(" mihomo 内核版本（选择右侧操作执行） ")
	meta := tview.NewTextView().SetDynamicColors(true)
	status := tview.NewTextView().SetDynamicColors(true)
	refresh := tview.NewButton(" 刷新版本列表 ")
	latest := tview.NewButton(" 更新到最新稳定版 ")
	var versions []mihomotui.MihomoVersionInfo
	var canManage bool
	busy := false
	var load func()
	var selectVersion func(row, col int)

	setStatus := func(text string) { status.SetText(text) }
	pollDownload := func(client *mihomotui.IPCClient, version string, row int, activate bool) {
		go func() {
			var err error
			for {
				time.Sleep(250 * time.Millisecond)
				p, e := client.IPCGetMihomoUpgradeProgress()
				if e != nil {
					err = e
					break
				}
				app.QueueUpdateDraw(func() {
					text := fmt.Sprintf("[yellow::b]下载 %d%%[-]", p.Percent)
					if p.TotalBytes <= 0 && p.DownloadedBytes > 0 {
						text = "[yellow::b]已下载 " + mihomotui.FormatSize(p.DownloadedBytes) + "[-]"
					}
					if p.Status == "extracting" {
						text = "[yellow::b]正在解压[-]"
					}
					if p.Status == "done" {
						text = "[green::b]已下载[-]"
					}
					table.SetCell(row, 3, tview.NewTableCell(text))
					setStatus(" [yellow]●[-] " + p.Message)
				})
				if p.Status == "done" {
					break
				}
				if p.Status == "error" {
					err = fmt.Errorf("%s", p.Message)
					break
				}
			}
			if err == nil && activate {
				err = client.IPCActivateMihomoVersion(version)
			}
			app.QueueUpdateDraw(func() {
				busy = false
				if err != nil {
					setStatus(" [red]●[-] 操作失败: " + err.Error())
				} else if activate {
					setStatus(" [green]●[-] 已切换到 v" + version)
				} else {
					setStatus(" [green]●[-] 已下载 v" + version + "，请点击“切换”启用")
				}
				load()
			})
		}()
	}

	load = func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { setStatus(" [red]●[-] IPC 连接失败: " + err.Error()) })
				return
			}
			info, infoErr := client.IPCGetDaemonInfo()
			result, err := client.IPCGetMihomoVersions()
			app.QueueUpdateDraw(func() {
				if infoErr == nil {
					canManage = info.CanManageMihomo
					refresh.SetDisabled(!canManage)
					latest.SetDisabled(!canManage)
				}
				table.Clear()
				for col, text := range []string{"版本", "发布日期", "状态", "操作", "删除"} {
					table.SetCell(0, col, tview.NewTableCell("[::b]"+text).SetSelectable(false))
				}
				if err != nil {
					setStatus(" [red]●[-] 读取版本失败: " + err.Error())
					return
				}
				versions = result.Versions
				meta.SetText(fmt.Sprintf("来源：%s  缓存检查：%s%s", result.Source, result.CheckedAt, func() string {
					if result.LastError != "" {
						return "\n[yellow]最近刷新失败：[-]" + result.LastError
					}
					return ""
				}()))
				for i, v := range versions {
					row := i + 1
					state := "未下载"
					action := "[::b]下载"
					remove := ""
					if v.Active {
						state = "[green]正在使用[-]"
						action = "[gray]—[-]"
					} else if v.Downloaded {
						state = "[blue]已下载[-]"
						action = "[::b]切换"
						remove = "[red::b]删除[-]"
					}
					if v.Prerelease {
						state += " · 预发布"
					}
					if !canManage && !v.Active {
						action = "[gray]仅 root 可操作[-]"
						remove = ""
					}
					table.SetCell(row, 0, tview.NewTableCell("v"+v.Version))
					table.SetCell(row, 1, tview.NewTableCell(v.PublishedAt))
					table.SetCell(row, 2, tview.NewTableCell(state))
					actionCell := tview.NewTableCell(action)
					if !v.Active {
						actionCell.SetClickedFunc(func() bool { selectVersion(row, 3); return true })
					}
					deleteCell := tview.NewTableCell(remove)
					if remove != "" {
						deleteCell.SetClickedFunc(func() bool { selectVersion(row, 4); return true })
					}
					table.SetCell(row, 3, actionCell)
					table.SetCell(row, 4, deleteCell)
				}
			})
		}()
	}
	selectVersion = func(row, col int) {
		if row <= 0 || row > len(versions) || busy {
			return
		}
		if !canManage {
			setStatus(" [yellow]●[-] 当前身份仅可查看；内核操作需要 root")
			return
		}
		v := versions[row-1]
		client, err := mihomotui.GetIPCClient()
		if err != nil {
			setStatus(" [red]●[-] IPC 连接失败: " + err.Error())
			return
		}
		switch col {
		case 3:
			if v.Active {
				return
			}
			busy = true
			if v.Downloaded {
				go func() {
					err := client.IPCActivateMihomoVersion(v.Version)
					app.QueueUpdateDraw(func() {
						busy = false
						if err != nil {
							setStatus(" [red]●[-] 切换失败: " + err.Error())
						} else {
							setStatus(" [green]●[-] 已切换到 v" + v.Version)
						}
						load()
					})
				}()
			} else {
				table.SetCell(row, 3, tview.NewTableCell("[yellow::b]下载 0%[-]"))
				if err := client.IPCDownloadMihomoVersion(v.Version); err != nil {
					busy = false
					setStatus(" [red]●[-] 下载失败: " + err.Error())
					return
				}
				pollDownload(client, v.Version, row, false)
			}
		case 4:
			if !v.Downloaded || v.Active {
				return
			}
			confirm := tview.NewModal().SetText("彻底删除 v" + v.Version + "？此操作无法撤销。").AddButtons([]string{"删除", "取消"})
			confirm.SetDoneFunc(func(i int, _ string) {
				modalHost.RemovePage("kernel-delete-confirm")
				app.SetFocus(table)
				if i != 0 {
					return
				}
				busy = true
				go func() {
					err := client.IPCDeleteMihomoVersion(v.Version)
					app.QueueUpdateDraw(func() {
						busy = false
						if err != nil {
							setStatus(" [red]●[-] 删除失败: " + err.Error())
						} else {
							setStatus(" [green]●[-] 已删除 v" + v.Version)
						}
						load()
					})
				}()
			})
			modalHost.AddPage("kernel-delete-confirm", confirm, true, true)
			app.SetFocus(confirm)
		}
	}
	table.SetSelectedFunc(selectVersion)
	refresh.SetSelectedFunc(func() {
		if !canManage || busy {
			return
		}
		busy = true
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				_, err = client.IPCRefreshMihomoVersions()
			}
			app.QueueUpdateDraw(func() {
				busy = false
				if err != nil {
					setStatus(" [red]●[-] 刷新失败: " + err.Error())
				}
				load()
			})
		}()
	})
	latest.SetSelectedFunc(func() {
		if !canManage || busy {
			return
		}
		for i, v := range versions {
			if !v.Prerelease {
				table.Select(i+1, 3)
				selectVersion(i+1, 3)
				return
			}
		}
		setStatus(" [yellow]●[-] 请先刷新版本列表")
	})
	buttons := tview.NewFlex().AddItem(refresh, 0, 1, true).AddItem(latest, 0, 1, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(meta, 2, 0, false).AddItem(table, 0, 1, true).AddItem(status, 1, 0, false).AddItem(buttons, 1, 0, false)
	modalHost.AddPage("kernel-content", root, true, true)
	load()
	return modalHost
}

func newExternalResourceView(app *tview.Application) tview.Primitive {
	list := tview.NewTable().SetFixed(1, 0)
	list.SetBorder(true).SetTitle(" GeoIP / GeoSite 外部资源 ")
	status := tview.NewTextView().SetDynamicColors(true)
	refresh := tview.NewButton(" 刷新状态 ")
	download := tview.NewButton(" 下载/更新资源 ")
	var canManage bool
	load := func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { status.SetText(" [red]●[-] IPC 连接失败: " + err.Error()) })
				return
			}
			info, _ := client.IPCGetDaemonInfo()
			resources, err := client.IPCCheckExternalResources()
			app.QueueUpdateDraw(func() {
				canManage = info != nil && info.CanManageMihomo
				download.SetDisabled(!canManage)
				list.Clear()
				list.SetCell(0, 0, tview.NewTableCell("[::b]资源"))
				list.SetCell(0, 1, tview.NewTableCell("[::b]状态"))
				list.SetCell(0, 2, tview.NewTableCell("[::b]大小"))
				if err != nil {
					status.SetText(" [red]●[-] 读取资源状态失败: " + err.Error())
					return
				}
				for i, r := range resources {
					state := "[red]缺失[-]"
					if r.Exists {
						state = "[green]可用[-]"
					}
					list.SetCell(i+1, 0, tview.NewTableCell(r.Name))
					list.SetCell(i+1, 1, tview.NewTableCell(state))
					list.SetCell(i+1, 2, tview.NewTableCell(mihomotui.FormatSize(r.Size)))
				}
			})
		}()
	}
	refresh.SetSelectedFunc(load)
	download.SetSelectedFunc(func() {
		if !canManage {
			return
		}
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				err = client.IPCDownloadExternalResources()
			}
			app.QueueUpdateDraw(func() {
				if err != nil {
					status.SetText(" [red]●[-] 下载失败: " + err.Error())
				} else {
					status.SetText(" [green]●[-] 外部资源已更新")
				}
				load()
			})
		}()
	})
	load()
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(list, 0, 1, true).AddItem(status, 1, 0, false).AddItem(tview.NewFlex().AddItem(refresh, 0, 1, true).AddItem(download, 0, 1, false), 1, 0, false)
}
