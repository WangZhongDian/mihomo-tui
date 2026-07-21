package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// NewResourcePage is the dedicated management page for downloadable runtime
// resources. Settings intentionally contains configuration only.
func NewResourcePage(app *tview.Application) tview.Primitive {
	pages := tview.NewPages()

	kernel := newKernelResourceView(app)
	external := newExternalResourceView(app)
	pageByName := map[string]tview.Primitive{"kernel": kernel, "external": external}
	pages.AddPage("kernel", kernel, true, true)
	pages.AddPage("external", external, true, false)
	// Pages does not automatically give focus to a child after SwitchToPage.
	// Without this explicit delegation, the external-resource URL field looks
	// editable but key presses stay on the hidden tab/page container.
	pages.Focus(func(p tview.Primitive) { app.SetFocus(p) })
	show := func(name string) {
		pages.SwitchToPage(name)
		if target := pageByName[name]; target != nil {
			app.SetFocus(target)
		}
	}
	kernelBtn := tview.NewButton(" 内核管理 ").SetSelectedFunc(func() { show("kernel") })
	externalBtn := tview.NewButton(" 外部资源 ").SetSelectedFunc(func() { show("external") })
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
	manualImport := tview.NewButton(" 扫描手动放置内核 ")
	var versions []mihomotui.MihomoVersionInfo
	var canManage bool
	var manualPath string
	busy := false
	var load func()
	var selectVersion func(row, col int)

	setStatus := func(text string) { status.SetText(text) }
	assetURL := func(version string) string {
		for _, info := range versions {
			if info.Version == version {
				return info.AssetURL
			}
		}
		return ""
	}
	var showManualHelp func(version, downloadURL string, cause error)
	showManualHelp = func(version, downloadURL string, cause error) {
		path := manualPath
		if path == "" {
			path = mihomotui.ManualMihomoImportPath()
		}
		text := "[red::b]内核操作失败[-]\n" + cause.Error() + "\n\n"
		if version != "" {
			text += "目标版本：v" + version + "\n"
		}
		if downloadURL != "" {
			text += "可手动下载地址：" + mihomotui.RedactURL(downloadURL) + "\n"
		}
		text += "请下载已解压的 mihomo 可执行文件，放置为：\n" + path + "\n\n要求：普通文件、非符号链接、可执行；扫描时将收紧权限并验证实际版本。"
		modal := tview.NewModal().SetText(text).AddButtons([]string{"扫描本地文件", "关闭"})
		modal.SetDoneFunc(func(index int, _ string) {
			modalHost.RemovePage("kernel-manual-help")
			app.SetFocus(table)
			if index != 0 || !canManage || busy {
				return
			}
			busy = true
			go func() {
				client, err := mihomotui.GetIPCClient()
				var imported *mihomotui.MihomoVersionInfo
				if err == nil {
					imported, err = client.IPCImportManualMihomo()
				}
				app.QueueUpdateDraw(func() {
					busy = false
					if err != nil {
						setStatus(" [red]●[-] 扫描手动内核失败: " + err.Error())
						showManualHelp(version, downloadURL, err)
					} else {
						setStatus(" [green]●[-] 已导入手动内核 v" + imported.Version)
						load()
					}
				})
			}()
		})
		modalHost.RemovePage("kernel-manual-help")
		modalHost.AddPage("kernel-manual-help", modal, true, true)
		app.SetFocus(modal)
	}

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
					showManualHelp(version, assetURL(version), err)
				} else if activate {
					setStatus(" [green]●[-] 已切换到 v" + version)
					load()
				} else {
					setStatus(" [green]●[-] 已下载 v" + version + "，请点击“切换”启用")
					load()
				}
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
					manualImport.SetDisabled(!canManage)
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
				manualPath = result.ManualImportPath
				meta.SetText(fmt.Sprintf("来源：%s  缓存检查：%s\n手动内核放置路径：%s%s", result.Source, result.CheckedAt, manualPath, func() string {
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
					if v.Manual {
						state += " · 手动导入"
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
					showManualHelp(v.Version, v.AssetURL, err)
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
			modalHost.RemovePage("kernel-delete-confirm")
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
					showManualHelp("", "", err)
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
	manualImport.SetSelectedFunc(func() {
		showManualHelp("", "", fmt.Errorf("请先将已解压的 mihomo 可执行文件放置到指定路径，再扫描"))
	})
	buttons := tview.NewFlex().AddItem(refresh, 0, 1, true).AddItem(latest, 0, 1, false).AddItem(manualImport, 0, 1, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(meta, 3, 0, false).AddItem(table, 0, 1, true).AddItem(status, 1, 0, false).AddItem(buttons, 1, 0, false)
	modalHost.AddPage("kernel-content", root, true, true)
	load()
	return modalHost
}

func newExternalResourceView(app *tview.Application) tview.Primitive {
	modalHost := tview.NewPages()
	// Do not put editable URL fields and action buttons in the same table/grid
	// row. On narrow terminals tview gives every zero-width grid column a share
	// of the remaining width, causing action cells to visually cover the URL.
	// Each resource therefore uses a compact card: status, full-width URL, then
	// a separate action row. The URL field always owns an entire line.
	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.SetBorder(true).SetTitle(" 外部资源 ")
	status := tview.NewTextView().SetDynamicColors(true)
	refresh := tview.NewButton(" 刷新状态 ")
	inputs := map[string]*tview.InputField{}
	stateViews := map[string]*tview.TextView{}
	pathViews := map[string]*tview.TextView{}
	saveButtons := map[string]*tview.Button{}
	updateButtons := map[string]*tview.Button{}
	scanButtons := map[string]*tview.Button{}
	resetButtons := map[string]*tview.Button{}
	resources := map[string]mihomotui.ExternalResourceInfo{}
	// dirtyURLs preserves a user edit across background status refreshes.  It
	// must not rely on HasFocus(): the first URL field intentionally receives
	// focus when the page opens, and using HasFocus previously skipped the first
	// server value entirely, leaving GeoIP visually blank.
	dirtyURLs := map[string]bool{}
	syncingURLs := false
	var canManage bool
	busy := map[string]bool{}
	var load func()

	for _, key := range []string{"geoip", "geosite"} {
		name := "GeoIP"
		if key == "geosite" {
			name = "GeoSite"
		}
		stateViews[key] = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
		pathViews[key] = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
		inputs[key] = tview.NewInputField().SetLabel("下载 URL: ").SetFieldWidth(0)
		inputs[key].SetChangedFunc(func(_ string) {
			if !syncingURLs {
				dirtyURLs[key] = true
			}
		})
		saveButtons[key] = tview.NewButton(" 保存 URL ")
		updateButtons[key] = tview.NewButton(" 强制更新 ")
		scanButtons[key] = tview.NewButton(" 扫描本地文件 ")
		resetButtons[key] = tview.NewButton(" 恢复默认 URL ")

		header := tview.NewTextView().SetDynamicColors(true).SetText("[::b]" + name + "[-]  ")
		header.SetBorderPadding(0, 0, 1, 0)
		infoLine := tview.NewFlex().
			AddItem(header, 12, 0, false).
			AddItem(stateViews[key], 0, 1, false)
		urlLine := tview.NewFlex().AddItem(inputs[key], 0, 1, true)
		actions := tview.NewFlex().
			AddItem(saveButtons[key], 14, 0, false).
			AddItem(updateButtons[key], 16, 0, false).
			AddItem(scanButtons[key], 20, 0, false).
			AddItem(resetButtons[key], 18, 0, false).
			AddItem(tview.NewBox(), 0, 1, false)
		card := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(infoLine, 1, 0, false).
			AddItem(urlLine, 1, 0, true).
			AddItem(actions, 1, 0, false).
			AddItem(pathViews[key], 2, 0, false)
		card.SetBorder(true).SetTitle(" " + name + " ")
		content.AddItem(card, 6, 0, key == "geoip")
	}

	var showManualHelp func(key string, cause error)
	showManualHelp = func(key string, cause error) {
		info, ok := resources[key]
		if !ok {
			return
		}
		text := "[red::b]" + info.Name + " 更新/验证失败[-]\n" + cause.Error() + "\n\n"
		text += "下载地址：" + mihomotui.RedactURL(info.URL) + "\n"
		text += "请手动下载 " + info.Name + " 文件并放置为：\n" + info.Path + "\n\n"
		text += "文件名必须保持为 " + filepath.Base(info.Path) + "；要求为非空普通文件、非符号链接，且不能允许组/其他用户写入。"
		modal := tview.NewModal().SetText(text).AddButtons([]string{"扫描本地文件", "关闭"})
		modal.SetDoneFunc(func(index int, _ string) {
			modalHost.RemovePage("external-manual-help")
			app.SetFocus(inputs[key])
			if index != 0 || !canManage || busy[key] {
				return
			}
			busy[key] = true
			go func() {
				client, err := mihomotui.GetIPCClient()
				if err == nil {
					_, err = client.IPCScanExternalResource(key)
				}
				app.QueueUpdateDraw(func() {
					busy[key] = false
					if err != nil {
						status.SetText(" [red]●[-] 扫描失败: " + err.Error())
						showManualHelp(key, err)
					} else {
						status.SetText(" [green]●[-] " + info.Name + " 手动文件已识别")
						load()
					}
				})
			}()
		})
		modalHost.RemovePage("external-manual-help")
		modalHost.AddPage("external-manual-help", modal, true, true)
		app.SetFocus(modal)
	}

	load = func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { status.SetText(" [red]●[-] IPC 连接失败: " + err.Error()) })
				return
			}
			info, infoErr := client.IPCGetDaemonInfo()
			list, listErr := client.IPCCheckExternalResources()
			app.QueueUpdateDraw(func() {
				canManage = infoErr == nil && info != nil && info.CanManageResources
				for _, key := range []string{"geoip", "geosite"} {
					saveButtons[key].SetDisabled(!canManage || busy[key])
					updateButtons[key].SetDisabled(!canManage || busy[key])
					scanButtons[key].SetDisabled(!canManage || busy[key])
					resetButtons[key].SetDisabled(!canManage || busy[key])
				}
				if listErr != nil {
					status.SetText(" [red]●[-] 读取资源状态失败: " + listErr.Error())
					return
				}
				resources = map[string]mihomotui.ExternalResourceInfo{}
				for _, resource := range list {
					resources[resource.Key] = resource
					state := "[red]缺失[-]"
					if resource.Valid {
						state = "[green]可用[-] " + mihomotui.FormatSize(resource.Size)
					} else if resource.Exists {
						state = "[yellow]文件无效[-]"
					}
					if !resource.ModTime.IsZero() {
						state += "  [gray]最近下载/更新：[-]" + resource.ModTime.Local().Format("2006-01-02 15:04:05")
					}
					if resource.LastError != "" {
						state += "\n[yellow]最近问题：[-]" + resource.LastError
					}
					if view := stateViews[resource.Key]; view != nil {
						view.SetText(state)
					}
					if view := pathViews[resource.Key]; view != nil {
						view.SetText("[gray]本地：[-]" + resource.Path)
					}
					// Always hydrate a clean input, including the initially focused
					// GeoIP field. Only retain text when the user has an unsaved edit.
					if input := inputs[resource.Key]; input != nil && !dirtyURLs[resource.Key] {
						syncingURLs = true
						input.SetText(resource.URL)
						syncingURLs = false
					}
				}
			})
		}()
	}

	for _, key := range []string{"geoip", "geosite"} {
		key := key
		saveButtons[key].SetSelectedFunc(func() {
			if !canManage || busy[key] {
				return
			}
			busy[key] = true
			value := inputs[key].GetText()
			go func() {
				client, err := mihomotui.GetIPCClient()
				if err == nil {
					err = client.IPCSetExternalResourceURL(key, value)
				}
				app.QueueUpdateDraw(func() {
					busy[key] = false
					if err != nil {
						status.SetText(" [red]●[-] 保存 URL 失败: " + err.Error())
					} else {
						status.SetText(" [green]●[-] 下载 URL 已保存；需要时点击“强制更新”")
					}
					load()
				})
			}()
		})
		updateButtons[key].SetSelectedFunc(func() {
			if !canManage || busy[key] {
				return
			}
			busy[key] = true
			status.SetText(" [yellow]●[-] 正在强制更新 " + key + "...")
			go func() {
				client, err := mihomotui.GetIPCClient()
				if err == nil {
					_, err = client.IPCUpdateExternalResource(key)
				}
				app.QueueUpdateDraw(func() {
					busy[key] = false
					if err != nil {
						status.SetText(" [red]●[-] 更新失败: " + err.Error())
						load()
						showManualHelp(key, err)
						return
					}
					status.SetText(" [green]●[-] 资源已强制更新")
					load()
				})
			}()
		})
		scanButtons[key].SetSelectedFunc(func() {
			showManualHelp(key, fmt.Errorf("请先将手动下载的文件放置到指定路径，再扫描"))
		})
		resetButtons[key].SetSelectedFunc(func() {
			if !canManage || busy[key] {
				return
			}
			defaultURL := mihomotui.DefaultGeoIPDownloadURL
			resourceName := "GeoIP"
			if key == "geosite" {
				defaultURL = mihomotui.DefaultGeoSiteDownloadURL
				resourceName = "GeoSite"
			}
			confirm := tview.NewModal().
				SetText("恢复 " + resourceName + " 的默认下载 URL？\n不会立即下载或覆盖当前本地资源。").
				AddButtons([]string{"恢复默认", "取消"})
			confirm.SetDoneFunc(func(index int, _ string) {
				modalHost.RemovePage("external-reset-url-confirm")
				app.SetFocus(inputs[key])
				if index != 0 {
					return
				}
				busy[key] = true
				go func() {
					client, err := mihomotui.GetIPCClient()
					if err == nil {
						err = client.IPCSetExternalResourceURL(key, defaultURL)
					}
					app.QueueUpdateDraw(func() {
						busy[key] = false
						if err != nil {
							status.SetText(" [red]●[-] 恢复默认 URL 失败: " + err.Error())
						} else {
							dirtyURLs[key] = false
							syncingURLs = true
							inputs[key].SetText(defaultURL)
							syncingURLs = false
							status.SetText(" [green]●[-] " + resourceName + " 已恢复默认下载 URL；需要时点击“强制更新”")
						}
						load()
					})
				}()
			})
			modalHost.RemovePage("external-reset-url-confirm")
			modalHost.AddPage("external-reset-url-confirm", confirm, true, true)
			app.SetFocus(confirm)
		})
	}
	refresh.SetSelectedFunc(load)
	root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(content, 0, 1, true).AddItem(status, 1, 0, false).AddItem(refresh, 1, 0, false)
	modalHost.AddPage("external-content", root, true, true)
	// modalHost is the primitive selected by the resource-page tab. Delegate
	// initial and restored focus to the first URL field so it is immediately
	// editable; tview otherwise leaves focus on Pages, which consumes no text.
	modalHost.Focus(func(_ tview.Primitive) { app.SetFocus(inputs["geoip"]) })
	load()
	return modalHost
}
