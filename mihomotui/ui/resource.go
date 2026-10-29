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
	kernelBtn := tview.NewButton(" " + T("resources.kernel.tab") + " ").SetSelectedFunc(func() { show("kernel") })
	externalBtn := tview.NewButton(" " + T("resources.external.tab") + " ").SetSelectedFunc(func() { show("external") })
	tabs := tview.NewFlex().AddItem(kernelBtn, 0, 1, true).AddItem(externalBtn, 0, 1, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(tabs, 1, 0, true).AddItem(pages, 0, 1, true)
}

func newKernelResourceView(app *tview.Application) tview.Primitive {
	modalHost := tview.NewPages()
	table := tview.NewTable().SetSelectable(true, true).SetFixed(1, 0)
	table.SetBorder(true).SetTitle(" " + T("resources.kernel.versions_title") + " ")
	meta := tview.NewTextView().SetDynamicColors(true)
	status := tview.NewTextView().SetDynamicColors(true)
	refresh := tview.NewButton(" " + T("resources.kernel.refresh_versions") + " ")
	latest := tview.NewButton(" " + T("resources.kernel.update_latest") + " ")
	manualImport := tview.NewButton(" " + T("resources.kernel.scan_manual") + " ")
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
		text := "[red::b]" + T("resources.kernel.operation_failed") + "[-]\n" + cause.Error() + "\n\n"
		if version != "" {
			text += Tf("resources.kernel.target_version", map[string]any{"Version": version}) + "\n"
		}
		if downloadURL != "" {
			text += Tf("resources.kernel.manual_download_url", map[string]any{"URL": mihomotui.RedactURL(downloadURL)}) + "\n"
		}
		text += Tf("resources.kernel.manual_help", map[string]any{"Path": path})
		modal := tview.NewModal().SetText(text).AddButtons([]string{T("resources.scan_local"), T("common.close")})
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
						setStatus(" [red]●[-] " + Tf("resources.kernel.scan_manual_failed", map[string]any{"Error": err.Error()}))
						showManualHelp(version, downloadURL, err)
					} else {
						setStatus(" [green]●[-] " + Tf("resources.kernel.manual_imported", map[string]any{"Version": imported.Version}))
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
					text := "[yellow::b]" + Tf("resources.kernel.download_percent", map[string]any{"Percent": p.Percent}) + "[-]"
					if p.TotalBytes <= 0 && p.DownloadedBytes > 0 {
						text = "[yellow::b]" + Tf("resources.kernel.downloaded_size", map[string]any{"Size": mihomotui.FormatSize(p.DownloadedBytes)}) + "[-]"
					}
					if p.Status == "extracting" {
						text = "[yellow::b]" + T("resources.kernel.extracting") + "[-]"
					}
					if p.Status == "done" {
						text = "[green::b]" + T("resources.kernel.downloaded") + "[-]"
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
					setStatus(" [red]●[-] " + Tf("resources.operation_failed", map[string]any{"Error": err.Error()}))
					showManualHelp(version, assetURL(version), err)
				} else if activate {
					setStatus(" [green]●[-] " + Tf("resources.kernel.switched", map[string]any{"Version": version}))
					load()
				} else {
					setStatus(" [green]●[-] " + Tf("resources.kernel.download_activate", map[string]any{"Version": version}))
					load()
				}
			})
		}()
	}

	load = func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() { setStatus(" [red]●[-] " + Tf("resources.ipc_failed", map[string]any{"Error": err.Error()})) })
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
				for col, text := range []string{T("resources.kernel.column.version"), T("resources.kernel.column.published"), T("resources.kernel.column.status"), T("resources.kernel.column.action"), T("common.delete")} {
					table.SetCell(0, col, tview.NewTableCell("[::b]"+text).SetSelectable(false))
				}
				if err != nil {
					setStatus(" [red]●[-] " + Tf("resources.kernel.read_failed", map[string]any{"Error": err.Error()}))
					return
				}
				versions = result.Versions
				manualPath = result.ManualImportPath
				meta.SetText(Tf("resources.kernel.meta", map[string]any{"Source": result.Source, "CheckedAt": result.CheckedAt, "Path": manualPath, "LastError": func() string {
					if result.LastError != "" {
						return "\n[yellow]" + T("resources.kernel.last_refresh_failed") + "[-]" + result.LastError
					}
					return ""
				}()}))
				for i, v := range versions {
					row := i + 1
					state := T("resources.kernel.not_downloaded")
					action := "[::b]" + T("resources.kernel.download")
					remove := ""
					if v.Active {
						state = "[green]" + T("resources.kernel.in_use") + "[-]"
						action = "[gray]—[-]"
					} else if v.Downloaded {
						state = "[blue]" + T("resources.kernel.downloaded") + "[-]"
						action = "[::b]" + T("resources.kernel.activate")
						remove = "[red::b]" + T("common.delete") + "[-]"
					}
					if v.Prerelease {
						state += " · " + T("resources.kernel.prerelease")
					}
					if v.Manual {
						state += " · " + T("resources.kernel.manual")
					}
					if !canManage && !v.Active {
						action = "[gray]" + T("resources.kernel.root_only") + "[-]"
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
			setStatus(" [yellow]●[-] " + T("resources.kernel.view_only"))
			return
		}
		v := versions[row-1]
		client, err := mihomotui.GetIPCClient()
		if err != nil {
			setStatus(" [red]●[-] " + Tf("resources.ipc_failed", map[string]any{"Error": err.Error()}))
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
							setStatus(" [red]●[-] " + Tf("resources.kernel.activate_failed", map[string]any{"Error": err.Error()}))
						} else {
							setStatus(" [green]●[-] " + Tf("resources.kernel.switched", map[string]any{"Version": v.Version}))
						}
						load()
					})
				}()
			} else {
				table.SetCell(row, 3, tview.NewTableCell("[yellow::b]"+Tf("resources.kernel.download_percent", map[string]any{"Percent": 0})+"[-]"))
				if err := client.IPCDownloadMihomoVersion(v.Version); err != nil {
					busy = false
					setStatus(" [red]●[-] " + Tf("resources.kernel.download_failed", map[string]any{"Error": err.Error()}))
					showManualHelp(v.Version, v.AssetURL, err)
					return
				}
				pollDownload(client, v.Version, row, false)
			}
		case 4:
			if !v.Downloaded || v.Active {
				return
			}
			confirm := tview.NewModal().SetText(Tf("resources.kernel.delete_confirm", map[string]any{"Version": v.Version})).AddButtons([]string{T("common.delete"), T("common.cancel")})
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
							setStatus(" [red]●[-] " + Tf("resources.delete_failed", map[string]any{"Error": err.Error()}))
						} else {
							setStatus(" [green]●[-] " + Tf("resources.kernel.deleted", map[string]any{"Version": v.Version}))
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
					setStatus(" [red]●[-] " + Tf("resources.refresh_failed", map[string]any{"Error": err.Error()}))
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
		setStatus(" [yellow]●[-] " + T("resources.kernel.refresh_first"))
	})
	manualImport.SetSelectedFunc(func() {
		showManualHelp("", "", fmt.Errorf("%s", T("resources.kernel.manual_scan_hint")))
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
	content.SetBorder(true).SetTitle(" " + T("resources.external.tab") + " ")
	status := tview.NewTextView().SetDynamicColors(true)
	refresh := tview.NewButton(" " + T("resources.external.refresh_status") + " ")
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
		inputs[key] = tview.NewInputField().SetLabel(T("resources.external.download_url") + ": ").SetFieldWidth(0)
		inputs[key].SetChangedFunc(func(_ string) {
			if !syncingURLs {
				dirtyURLs[key] = true
			}
		})
		saveButtons[key] = tview.NewButton(" " + T("resources.external.save_url") + " ")
		updateButtons[key] = tview.NewButton(" " + T("resources.external.force_update") + " ")
		scanButtons[key] = tview.NewButton(" " + T("resources.scan_local") + " ")
		resetButtons[key] = tview.NewButton(" " + T("resources.external.restore_url") + " ")

		header := tview.NewTextView().SetDynamicColors(true).SetText("[::b]" + name + "[-]  ")
		header.SetBorderPadding(0, 0, 1, 0)
		infoLine := tview.NewFlex().
			AddItem(header, 12, 0, false).
			AddItem(stateViews[key], 0, 1, false)
		urlLine := tview.NewFlex().AddItem(inputs[key], 0, 1, true)
		// 按钮之间用 2 格空隙隔开，避免一片蓝色连在一起。
		actions := tview.NewFlex().
			AddItem(saveButtons[key], 14, 0, false).
			AddItem(nil, 2, 0, false).
			AddItem(updateButtons[key], 16, 0, false).
			AddItem(nil, 2, 0, false).
			AddItem(scanButtons[key], 20, 0, false).
			AddItem(nil, 2, 0, false).
			AddItem(resetButtons[key], 18, 0, false).
			AddItem(tview.NewBox(), 0, 1, false)
		card := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(infoLine, 1, 0, false).
			AddItem(urlLine, 1, 0, true).
			AddItem(nil, 1, 0, false).
			AddItem(actions, 1, 0, false).
			AddItem(nil, 1, 0, false).
			AddItem(pathViews[key], 2, 0, false)
		card.SetBorder(true).SetTitle(" " + name + " ")
		content.AddItem(card, 8, 0, key == "geoip")
	}

	var showManualHelp func(key string, cause error)
	showManualHelp = func(key string, cause error) {
		info, ok := resources[key]
		if !ok {
			return
		}
		text := "[red::b]" + Tf("resources.external.validation_failed", map[string]any{"Name": info.Name}) + "[-]\n" + cause.Error() + "\n\n"
		text += Tf("resources.external.download_address", map[string]any{"URL": mihomotui.RedactURL(info.URL)}) + "\n"
		text += Tf("resources.external.manual_download", map[string]any{"Name": info.Name, "Path": info.Path}) + "\n\n"
		text += Tf("resources.external.manual_requirements", map[string]any{"Filename": filepath.Base(info.Path)})
		modal := tview.NewModal().SetText(text).AddButtons([]string{T("resources.scan_local"), T("common.close")})
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
						status.SetText(" [red]●[-] " + Tf("resources.external.scan_failed", map[string]any{"Error": err.Error()}))
						showManualHelp(key, err)
					} else {
						status.SetText(" [green]●[-] " + Tf("resources.external.manual_recognized", map[string]any{"Name": info.Name}))
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
				app.QueueUpdateDraw(func() {
					status.SetText(" [red]●[-] " + Tf("resources.ipc_failed", map[string]any{"Error": err.Error()}))
				})
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
					status.SetText(" [red]●[-] " + Tf("resources.external.read_failed", map[string]any{"Error": listErr.Error()}))
					return
				}
				resources = map[string]mihomotui.ExternalResourceInfo{}
				for _, resource := range list {
					resources[resource.Key] = resource
					state := "[red]" + T("resources.external.missing") + "[-]"
					if resource.Valid {
						state = "[green]" + T("resources.external.available") + "[-] " + mihomotui.FormatSize(resource.Size)
					} else if resource.Exists {
						state = "[yellow]" + T("resources.external.invalid") + "[-]"
					}
					if !resource.ModTime.IsZero() {
						state += "  [gray]" + T("resources.external.last_updated") + "[-]" + resource.ModTime.Local().Format("2006-01-02 15:04:05")
					}
					if resource.LastError != "" {
						state += "\n[yellow]" + T("resources.external.last_issue") + "[-]" + resource.LastError
					}
					if view := stateViews[resource.Key]; view != nil {
						view.SetText(state)
					}
					if view := pathViews[resource.Key]; view != nil {
						view.SetText("[gray]" + T("resources.external.local_path") + "[-]" + resource.Path)
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
						status.SetText(" [red]●[-] " + Tf("resources.external.save_failed", map[string]any{"Error": err.Error()}))
					} else {
						status.SetText(" [green]●[-] " + T("resources.external.saved"))
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
			status.SetText(" [yellow]●[-] " + Tf("resources.external.updating", map[string]any{"Name": key}))
			go func() {
				client, err := mihomotui.GetIPCClient()
				if err == nil {
					_, err = client.IPCUpdateExternalResource(key)
				}
				app.QueueUpdateDraw(func() {
					busy[key] = false
					if err != nil {
						status.SetText(" [red]●[-] " + Tf("resources.external.update_failed", map[string]any{"Error": err.Error()}))
						load()
						showManualHelp(key, err)
						return
					}
					status.SetText(" [green]●[-] " + T("resources.external.updated"))
					load()
				})
			}()
		})
		scanButtons[key].SetSelectedFunc(func() {
			showManualHelp(key, fmt.Errorf("%s", T("resources.external.manual_scan_hint")))
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
				SetText(Tf("resources.external.restore_confirm", map[string]any{"Name": resourceName})).
				AddButtons([]string{T("resources.external.restore_default"), T("common.cancel")})
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
							status.SetText(" [red]●[-] " + Tf("resources.external.restore_failed", map[string]any{"Error": err.Error()}))
						} else {
							dirtyURLs[key] = false
							syncingURLs = true
							inputs[key].SetText(defaultURL)
							syncingURLs = false
							status.SetText(" [green]●[-] " + Tf("resources.external.restored", map[string]any{"Name": resourceName}))
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
