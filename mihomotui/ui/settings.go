package ui

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// buildProgressBar 生成文本进度条，percent 为 0-100
func buildProgressBar(percent int) string {
	width := 20
	filled := min(percent*width/100, width)
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

// inputBinding 设置页输入框绑定：apply 解析并应用输入到配置（返回错误表示输入无效），
// current 读取当前生效值的显示形式（用于内容比对与失败后恢复显示）。
type inputBinding struct {
	apply   func(fresh *mihomotui.Config, text string) error
	current func(c *mihomotui.Config) string
}

// NewSettingsPage 创建设置页面
func NewSettingsPage(app *tview.Application) tview.Primitive {
	activeTab := 0 // 0=系统设置, 1=mihomo设置, 2=关于

	// Tab 按钮
	tabSystem := tview.NewButton(" 系统设置 ")
	tabSystem.SetBorder(false)
	tabMihomo := tview.NewButton(" mihomo 设置 ")
	tabMihomo.SetBorder(false)
	tabAbout := tview.NewButton(" 关于 ")
	tabAbout.SetBorder(false)

	// Tab 栏
	tabs := tview.NewFlex().
		AddItem(tabSystem, 0, 1, true).
		AddItem(tabMihomo, 0, 1, true).
		AddItem(tabAbout, 0, 1, true)

	// 内容区域
	content := tview.NewFlex()

	// Pages 容器，用于支持弹窗覆盖
	settingsPages := tview.NewPages()
	settingsPages.Focus(func(p tview.Primitive) { app.SetFocus(p) })

	// 弹窗显示（提前定义供表单保存回调使用）
	showModal := func(title, message string) {
		modal := tview.NewModal().
			SetText(fmt.Sprintf("%s\n\n%s", title, message)).
			AddButtons([]string{"确认"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				settingsPages.HidePage("modal")
				settingsPages.RemovePage("modal")
			})
		settingsPages.AddPage("modal", modal, true, true)
	}

	cfg := mihomotui.GlobalConfig()

	// tview 的 AddDropDown 在设置初始选项时会同步触发一次 selected 回调
	// （SetCurrentOption 的既有行为），页面构建期间会产生并非用户操作的
	// 伪修改；构建完成前的保存请求一律忽略，全部表单构建结束后置为 true。
	pageReady := false

	// reverting 为 true 表示正在以编程方式恢复控件显示（保存失败回滚）；
	// 此期间控件的 changed/selected 回调不再触发保存，防止回滚递归。
	// tview 的 SetChecked/SetCurrentOption 在值变化时会同步触发回调，
	// 因此回滚必须通过 silentSet 进行（仅在 UI 线程使用）。
	reverting := false
	silentSet := func(fn func()) {
		reverting = true
		defer func() { reverting = false }()
		fn()
	}

	// 统一保存函数：异步 IPC 同步到服务端。
	// 以"读取最新 → 应用单字段变更 → 提交"的方式保存，
	// 避免陈旧页面快照整份覆盖服务端的并发修改；
	// 服务端校验/冲突失败时先执行 revert 恢复控件显示（保持界面与
	// 实际生效配置一致），再弹窗提示；保存成功但运行时应用失败时记录警告。
	doSave := func(fieldName string, mutate func(fresh *mihomotui.Config), revert func()) {
		if !pageReady || reverting {
			return
		}
		go func() {
			resp, err := mihomotui.MutateServerConfig(mutate)
			if err != nil {
				mihomotui.Warnf("保存 %s 失败: %v", fieldName, err)
				app.QueueUpdateDraw(func() {
					if revert != nil {
						revert()
					}
					showModal("保存失败", fmt.Sprintf("%s: %v", fieldName, err))
				})
				return
			}
			if !resp.Applied {
				mihomotui.Warnf("设置 %s 已保存，但应用失败（%s）: %s", fieldName, resp.ApplyStage, resp.ApplyError)
			}
			// 本地缓存已在提交成功后同步，以其为准刷新日志配置
			g := mihomotui.GlobalConfig()
			_ = mihomotui.InitLogger(g.LogDir, g.LogLevel)
			mihomotui.Infof("设置 %s 已保存", fieldName)
		}()
	}

	// 按标签查找表单控件，用于保存失败后的显示回滚（查找发生在 UI 线程）。
	findCheckbox := func(form *tview.Form, label string) *tview.Checkbox {
		for i := 0; i < form.GetFormItemCount(); i++ {
			if cb, ok := form.GetFormItem(i).(*tview.Checkbox); ok && cb.GetLabel() == label {
				return cb
			}
		}
		return nil
	}
	findDropDown := func(form *tview.Form, label string) *tview.DropDown {
		for i := 0; i < form.GetFormItemCount(); i++ {
			if dd, ok := form.GetFormItem(i).(*tview.DropDown); ok && dd.GetLabel() == label {
				return dd
			}
		}
		return nil
	}

	// bindInputSave 为表单输入框设置失焦保存：仅在内容变化时提交；
	// 输入无法解析或保存失败时恢复显示为当前生效值，避免界面与实际配置不一致。
	bindInputSave := func(form *tview.Form, bindings map[string]inputBinding) {
		for i := 0; i < form.GetFormItemCount(); i++ {
			field, ok := form.GetFormItem(i).(*tview.InputField)
			if !ok {
				continue
			}
			binding, ok := bindings[field.GetLabel()]
			if !ok {
				continue
			}
			inputField := field
			fieldBinding := binding
			inputField.SetFinishedFunc(func(key tcell.Key) {
				text := inputField.GetText()
				g := mihomotui.GlobalConfig()
				if text == fieldBinding.current(g) {
					return // 内容未变化（含失焦未编辑）
				}
				if err := fieldBinding.apply(g, text); err != nil {
					inputField.SetText(fieldBinding.current(mihomotui.GlobalConfig()))
					showModal("无效输入", fmt.Sprintf("%s: %v", inputField.GetLabel(), err))
					return
				}
				doSave(inputField.GetLabel(),
					func(fresh *mihomotui.Config) { _ = fieldBinding.apply(fresh, text) },
					func() { inputField.SetText(fieldBinding.current(mihomotui.GlobalConfig())) })
			})
		}
	}

	// portBinding 构造端口类输入框绑定：解析整数，失败时给出可读错误。
	portBinding := func(set func(fresh *mihomotui.Config, v int), get func(c *mihomotui.Config) int) inputBinding {
		return inputBinding{
			apply: func(fresh *mihomotui.Config, text string) error {
				v, err := strconv.Atoi(strings.TrimSpace(text))
				if err != nil {
					return fmt.Errorf("请输入有效数字")
				}
				set(fresh, v)
				return nil
			},
			current: func(c *mihomotui.Config) string { return strconv.Itoa(get(c)) },
		}
	}

	// 语言索引映射
	langIdx := 0
	if cfg.System.Language == "en" {
		langIdx = 1
	}
	// mihomo 日志级别索引映射
	logLevels := []string{"debug", "info", "warning", "error", "silent"}
	logIdx := 1
	for i, v := range logLevels {
		if v == cfg.Mihomo.LogLevel {
			logIdx = i
			break
		}
	}
	// app 日志级别索引映射
	appLogLevels := []string{"debug", "info", "warn", "error"}
	appLogIdx := 1
	for i, v := range appLogLevels {
		if v == cfg.LogLevel {
			appLogIdx = i
			break
		}
	}
	// 默认代理策略索引
	policyIdx := 0
	for i, v := range mihomotui.PolicyList {
		if v == cfg.DefaultProxyGroup {
			policyIdx = i
			break
		}
	}

	// 通过 IPC 从服务端获取工作目录
	workDir := mihomotui.GetConfigDir()
	if client, err := mihomotui.GetIPCClient(); err == nil {
		if dir, err := client.IPCGetConfigDir(); err == nil && dir != "" {
			workDir = dir
		} else {
			mihomotui.Errorf("获取工作目录失败: %v", err)
		}
	}

	// ---------- 系统设置 ----------
	// 注意：控件回调不再预写页面配置快照，"原始值"一律以本地缓存 GlobalConfig()
	// （每次保存成功后与服务端同步）为准；保存失败时据此恢复控件显示。
	systemForm := tview.NewForm()
	systemForm.AddCheckbox("开机启动", cfg.System.AutoStart, func(checked bool) {
		doSave("开机启动",
			func(fresh *mihomotui.Config) { fresh.System.AutoStart = checked },
			func() {
				if cb := findCheckbox(systemForm, "开机启动"); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().System.AutoStart) })
				}
			})
	}).
		AddCheckbox("系统代理", mihomotui.GetSystemProxyPreference(), func(checked bool) {
			if reverting {
				return
			}
			// 系统代理是当前 TUI 用户的本地偏好：写本用户环境变量 + 本地偏好文件，
			// 不写入 daemon 全局配置。
			if err := cfg.SetSystemProxyEnv(checked); err != nil {
				mihomotui.Warnf("系统代理环境变量设置失败: %v", err)
				// 注入失败：回滚偏好与显示，避免显示状态与实际不符
				if perr := mihomotui.SetSystemProxyPreference(!checked); perr != nil {
					mihomotui.Warnf("系统代理偏好回滚失败: %v", perr)
				}
				if cb := findCheckbox(systemForm, "系统代理"); cb != nil {
					silentSet(func() { cb.SetChecked(!checked) })
				}
				return
			}
			if err := mihomotui.SetSystemProxyPreference(checked); err != nil {
				mihomotui.Warnf("系统代理偏好保存失败: %v", err)
			}
		}).
		AddCheckbox("虚拟网卡模式", cfg.System.TUN, func(checked bool) {
			doSave("虚拟网卡模式",
				func(fresh *mihomotui.Config) { fresh.System.TUN = checked },
				func() {
					if cb := findCheckbox(systemForm, "虚拟网卡模式"); cb != nil {
						silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().System.TUN) })
					}
				})
		}).
		AddDropDown("语言", []string{"简体中文", "English"}, langIdx, func(option string, optionIndex int) {
			language := "zh-CN"
			if optionIndex == 1 {
				language = "en"
			}
			// 值未变化（初始化/回退/重复选择触发）时不保存
			if language == mihomotui.GlobalConfig().System.Language {
				return
			}
			doSave("语言",
				func(fresh *mihomotui.Config) { fresh.System.Language = language },
				func() {
					idx := 0
					if mihomotui.GlobalConfig().System.Language == "en" {
						idx = 1
					}
					if dd := findDropDown(systemForm, "语言"); dd != nil {
						silentSet(func() { dd.SetCurrentOption(idx) })
					}
				})
		}).
		AddDropDown("应用日志级别", []string{"DEBUG", "INFO", "WARN", "ERROR"}, appLogIdx, func(option string, optionIndex int) {
			level := appLogLevels[optionIndex]
			if level == mihomotui.GlobalConfig().LogLevel {
				return
			}
			doSave("应用日志级别",
				func(fresh *mihomotui.Config) { fresh.LogLevel = level },
				func() {
					cur := mihomotui.GlobalConfig().LogLevel
					idx := appLogIdx
					for i, v := range appLogLevels {
						if v == cur {
							idx = i
							break
						}
					}
					if dd := findDropDown(systemForm, "应用日志级别"); dd != nil {
						silentSet(func() { dd.SetCurrentOption(idx) })
					}
				})
		}).
		AddInputField("日志目录", cfg.LogDir, 50, nil, nil).
		AddInputField("工作目录", workDir, 50, func(text string, ch rune) bool { return false }, nil)

	// 系统设置输入框失焦保存（"工作目录"为只读字段，不在绑定表中）
	bindInputSave(systemForm, map[string]inputBinding{
		"日志目录": {
			apply: func(fresh *mihomotui.Config, text string) error {
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("日志目录不能为空")
				}
				fresh.LogDir = text
				return nil
			},
			current: func(c *mihomotui.Config) string { return c.LogDir },
		},
	})
	systemForm.SetBorder(true).SetTitle(" 系统设置 ")

	// ---------- mihomo 设置 ----------
	mihomoForm := tview.NewForm()
	mihomoForm.AddInputField("HTTP 端口", strconv.Itoa(cfg.Mihomo.HTTPPort), 10, nil, nil).
		AddInputField("SOCKS5 端口", strconv.Itoa(cfg.Mihomo.SOCKS5Port), 10, nil, nil).
		AddInputField("混合端口", strconv.Itoa(cfg.Mihomo.MixedPort), 10, nil, nil).
		AddInputField("Redir 端口", strconv.Itoa(cfg.Mihomo.RedirPort), 10, nil, nil).
		AddInputField("TProxy 端口", strconv.Itoa(cfg.Mihomo.TProxyPort), 10, nil, nil).
		AddCheckbox("允许局域网", cfg.Mihomo.AllowLan, func(checked bool) {
			doSave("允许局域网",
				func(fresh *mihomotui.Config) { fresh.Mihomo.AllowLan = checked },
				func() {
					if cb := findCheckbox(mihomoForm, "允许局域网"); cb != nil {
						silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.AllowLan) })
					}
				})
		}).
		AddCheckbox("IPv6", cfg.Mihomo.IPv6, func(checked bool) {
			doSave("IPv6",
				func(fresh *mihomotui.Config) { fresh.Mihomo.IPv6 = checked },
				func() {
					if cb := findCheckbox(mihomoForm, "IPv6"); cb != nil {
						silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.IPv6) })
					}
				})
		}).
		AddCheckbox("统一延迟", cfg.Mihomo.UnifiedDelay, func(checked bool) {
			doSave("统一延迟",
				func(fresh *mihomotui.Config) { fresh.Mihomo.UnifiedDelay = checked },
				func() {
					if cb := findCheckbox(mihomoForm, "统一延迟"); cb != nil {
						silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.UnifiedDelay) })
					}
				})
		}).
		AddCheckbox("自动透明代理", cfg.Mihomo.AutoRedirect, func(checked bool) {
			doSave("自动透明代理",
				func(fresh *mihomotui.Config) { fresh.Mihomo.AutoRedirect = checked },
				func() {
					if cb := findCheckbox(mihomoForm, "自动透明代理"); cb != nil {
						silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.AutoRedirect) })
					}
				})
		}).
		AddDropDown("日志级别", []string{"DEBUG", "INFO", "WARNING", "ERROR", "SILENT"}, logIdx, func(option string, optionIndex int) {
			level := logLevels[optionIndex]
			if level == mihomotui.GlobalConfig().Mihomo.LogLevel {
				return
			}
			doSave("日志级别",
				func(fresh *mihomotui.Config) { fresh.Mihomo.LogLevel = level },
				func() {
					cur := mihomotui.GlobalConfig().Mihomo.LogLevel
					idx := logIdx
					for i, v := range logLevels {
						if v == cur {
							idx = i
							break
						}
					}
					if dd := findDropDown(mihomoForm, "日志级别"); dd != nil {
						silentSet(func() { dd.SetCurrentOption(idx) })
					}
				})
		}).
		AddDropDown("代理默认策略", mihomotui.PolicyList, policyIdx, func(option string, optionIndex int) {
			if option == mihomotui.GlobalConfig().DefaultProxyGroup {
				return
			}
			doSave("代理默认策略",
				func(fresh *mihomotui.Config) { fresh.DefaultProxyGroup = option },
				func() {
					cur := mihomotui.GlobalConfig().DefaultProxyGroup
					idx := policyIdx
					for i, v := range mihomotui.PolicyList {
						if v == cur {
							idx = i
							break
						}
					}
					if dd := findDropDown(mihomoForm, "代理默认策略"); dd != nil {
						silentSet(func() { dd.SetCurrentOption(idx) })
					}
				})
		}).
		AddInputField("延迟测试链接", cfg.Mihomo.TestURL, 50, nil, nil)

	// mihomo 设置输入框失焦保存
	bindInputSave(mihomoForm, map[string]inputBinding{
		"HTTP 端口": portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.HTTPPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.HTTPPort }),
		"SOCKS5 端口": portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.SOCKS5Port = v },
			func(c *mihomotui.Config) int { return c.Mihomo.SOCKS5Port }),
		"混合端口": portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.MixedPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.MixedPort }),
		"Redir 端口": portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.RedirPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.RedirPort }),
		"TProxy 端口": portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.TProxyPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.TProxyPort }),
		"延迟测试链接": {
			apply: func(fresh *mihomotui.Config, text string) error {
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("测试链接不能为空")
				}
				fresh.Mihomo.TestURL = text
				return nil
			},
			current: func(c *mihomotui.Config) string { return c.Mihomo.TestURL },
		},
	})
	mihomoForm.SetBorder(true).SetTitle(" mihomo 设置 ")

	// ---------- mihomo 版本卡片 ----------
	currentVersion := ""
	latestVersion := ""
	hasNewVersion := false
	isChecking := false

	versionInfo := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	updateStatus := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	updateBtn := tview.NewButton("")
	updateBtn.SetBorder(false)

	refreshVersionCard := func() {
		var infoText string
		if currentVersion == "" {
			infoText = "\n[blue::b]mihomo[-:-:-]\n\n[" + mihomotui.ColorMuted + "]未安装[-]\n\n[" + mihomotui.ColorMuted + "]Meta Kernel[-]"
		} else {
			infoText = fmt.Sprintf(
				"\n[blue::b]mihomo[-:-:-]\n\n"+
					"[::b]v%s[-:-:-]\n\n"+
					"[%s]Meta Kernel[-]",
				mihomotui.ColorMuted,
				currentVersion,
			)
		}
		versionInfo.SetText(infoText)

		if isChecking {
			updateStatus.SetText(" [" + mihomotui.ColorWarn + "]●[-] 检查中... ")
			updateBtn.SetLabel(" 检查中 ")
		} else if currentVersion == "" {
			// 未安装：始终显示下载安装
			if latestVersion != "" {
				updateStatus.SetText(fmt.Sprintf(" [%s]●[-] 最新: v%s ", mihomotui.ColorWarn, latestVersion))
			} else {
				updateStatus.SetText(" [" + mihomotui.ColorMuted + "]●[-] 未安装 ")
			}
			updateBtn.SetLabel(" 下载安装 ")
		} else if hasNewVersion {
			updateStatus.SetText(fmt.Sprintf(" [%s]●[-] 最新: v%s ", mihomotui.ColorWarn, latestVersion))
			updateBtn.SetLabel(fmt.Sprintf(" 更新到 v%s ", latestVersion))
		} else if latestVersion != "" {
			updateStatus.SetText(" [" + mihomotui.ColorOK + "]●[-] 已是最新版本 ")
			updateBtn.SetLabel(" 检查更新 ")
		} else {
			updateStatus.SetText(" [" + mihomotui.ColorMuted + "]●[-] 点击检查 ")
			updateBtn.SetLabel(" 检查更新 ")
		}
	}

	// 启动时自动检测本地版本（通过 IPC）
	go func() {
		client, err := mihomotui.GetIPCClient()
		var cv string
		if err == nil {
			cv, _ = client.IPCGetMihomoVersion()
		}
		app.QueueUpdateDraw(func() {
			currentVersion = cv
			refreshVersionCard()
		})
	}()

	// 检查最新版本（异步，通过 IPC）
	checkVersion := func() {
		if isChecking {
			return
		}
		isChecking = true
		refreshVersionCard()

		go func() {
			client, err := mihomotui.GetIPCClient()
			var lv string
			if err == nil {
				lv, err = client.IPCGetMihomoLatestVersion()
			}

			app.QueueUpdateDraw(func() {
				isChecking = false
				latestVersion = lv

				if err != nil && lv == "" {
					hasNewVersion = false
					refreshVersionCard()
					showModal("检测失败", "无法获取最新版本信息，请检查网络连接。")
					return
				}

				if currentVersion == "" {
					// 未安装
					hasNewVersion = false
					refreshVersionCard()
					return
				}

				if currentVersion == lv {
					// 已是最新
					hasNewVersion = false
					refreshVersionCard()
					showModal("已是最新版本", fmt.Sprintf("当前版本: v%s", currentVersion))
					return
				}

				// 有新版本
				hasNewVersion = true
				refreshVersionCard()
			})
		}()
	}

	// 执行更新（异步，通过 IPC，带进度弹窗）
	doUpdate := func() {
		if isChecking || latestVersion == "" {
			return
		}
		isChecking = true
		refreshVersionCard()

		// 进度弹窗
		progressText := tview.NewTextView().
			SetTextAlign(tview.AlignCenter).
			SetText("准备下载...")
		progressFlex := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(tview.NewTextView().SetText("正在下载 mihomo 内核").SetTextAlign(tview.AlignCenter), 1, 0, false).
			AddItem(progressText, 1, 0, false)
		progressFlex.SetBorder(true).SetTitle(" 升级 ").SetTitleAlign(tview.AlignCenter)
		progressModalFrame := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(progressFlex, 4, 0, true).
				AddItem(nil, 0, 1, false), 50, 0, true).
			AddItem(nil, 0, 1, false)
		settingsPages.AddPage("upgrade-progress", progressModalFrame, true, true)

		go func() {
			client, err := mihomotui.GetIPCClient()
			if err != nil {
				app.QueueUpdateDraw(func() {
					settingsPages.HidePage("upgrade-progress")
					settingsPages.RemovePage("upgrade-progress")
					isChecking = false
					updateStatus.SetText(" [" + mihomotui.ColorError + "]●[-] 更新失败 ")
					showModal("更新失败", fmt.Sprintf("获取 IPC 客户端失败: %v", err))
					refreshVersionCard()
				})
				return
			}
			// 发起升级请求（服务端异步执行）
			if err := client.IPCUpgradeMihomo(latestVersion); err != nil {
				app.QueueUpdateDraw(func() {
					settingsPages.HidePage("upgrade-progress")
					settingsPages.RemovePage("upgrade-progress")
					isChecking = false
					updateStatus.SetText(" [" + mihomotui.ColorError + "]●[-] 更新失败 ")
					showModal("更新失败", fmt.Sprintf("启动升级失败: %v", err))
					refreshVersionCard()
				})
				return
			}

			// 轮询进度
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				progress, err := client.IPCGetMihomoUpgradeProgress()
				if err != nil {
					continue
				}
				app.QueueUpdateDraw(func() {
					switch progress.Status {
					case "downloading":
						bar := buildProgressBar(progress.Percent)
						progressText.SetText(fmt.Sprintf("%s %d%%", bar, progress.Percent))
					case "extracting":
						progressText.SetText("正在解压...")
					case "done":
						settingsPages.HidePage("upgrade-progress")
						settingsPages.RemovePage("upgrade-progress")
						isChecking = false
						currentVersion = latestVersion
						hasNewVersion = false
						updateStatus.SetText(" [" + mihomotui.ColorOK + "]●[-] 更新成功 ")
						showModal("更新成功", fmt.Sprintf("mihomo 已更新至 v%s", latestVersion))
						refreshVersionCard()
					case "error":
						settingsPages.HidePage("upgrade-progress")
						settingsPages.RemovePage("upgrade-progress")
						isChecking = false
						updateStatus.SetText(" [" + mihomotui.ColorError + "]●[-] 更新失败 ")
						showModal("更新失败", fmt.Sprintf("下载或安装失败: %s", progress.Message))
						refreshVersionCard()
					}
				})
				if progress.Status == "done" || progress.Status == "error" {
					break
				}
			}
		}()
	}

	updateBtn.SetSelectedFunc(func() {
		if currentVersion == "" {
			if latestVersion == "" {
				checkVersion()
			} else {
				doUpdate()
			}
		} else if hasNewVersion {
			doUpdate()
		} else {
			checkVersion()
		}
	})

	refreshVersionCard()

	versionBottom := tview.NewFlex().
		AddItem(updateStatus, 0, 1, false).
		AddItem(updateBtn, 18, 0, true)

	mihomoVersionCard := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(versionInfo, 0, 1, false).
		AddItem(versionBottom, 1, 0, true)
	mihomoVersionCard.SetBorder(true).SetTitle(" 版本信息 ")

	// mihomo 设置内容：左侧表单 + 右侧版本卡片
	mihomoContent := tview.NewFlex().
		AddItem(mihomoForm, 0, 3, true).
		AddItem(mihomoVersionCard, 24, 1, false)

	// ---------- 关于页面 ----------
	aboutText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(fmt.Sprintf(
			"\n\n"+
				"[::b]mihomo-tui[-:-:-]\n\n"+
				"版本: %s\n"+
				"Go 版本: %s\n\n"+
				"为 ["+mihomotui.ColorInfo+"]mihomo[-] 内核开发的终端 UI 配置工具\n\n"+
				"仓库: https://github.com/WangZhongDian/mihomo-tui\n\n"+
				"["+mihomotui.ColorMuted+"]© 2025 youmetme[-]",
			mihomotui.Version,
			runtime.Version(),
		))
	aboutText.SetBorder(true).SetTitle(" 关于 ")

	shutdownBtn := tview.NewButton(" 关闭后台服务并退出 ")
	shutdownBtn.SetBorder(false)
	shutdownBtn.SetSelectedFunc(func() {
		go func() {
			client, err := mihomotui.GetIPCClient()
			if err == nil {
				_ = client.IPCShutdownDaemon()
			}
			app.Stop()
		}()
	})

	aboutPage := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(aboutText, 0, 1, false).
		AddItem(shutdownBtn, 1, 0, true)

	// 更新 Tab 样式
	updateTabs := func() {
		if activeTab == 0 {
			tabSystem.SetLabel("[black:blue:b] 系统设置 [-:-:-]")
		} else {
			tabSystem.SetLabel(" 系统设置 ")
		}
		if activeTab == 1 {
			tabMihomo.SetLabel("[black:blue:b] mihomo 设置 [-:-:-]")
		} else {
			tabMihomo.SetLabel(" mihomo 设置 ")
		}
		if activeTab == 2 {
			tabAbout.SetLabel("[black:blue:b] 关于 [-:-:-]")
		} else {
			tabAbout.SetLabel(" 关于 ")
		}
	}

	// 切换内容
	switchContent := func(tab int) {
		activeTab = tab
		content.Clear()
		switch tab {
		case 0:
			content.AddItem(systemForm, 0, 1, true)
		case 1:
			content.AddItem(mihomoContent, 0, 1, true)
		case 2:
			content.AddItem(aboutPage, 0, 1, true)
		}
		updateTabs()
	}

	// Tab 点击事件
	tabSystem.SetSelectedFunc(func() { switchContent(0) })
	tabMihomo.SetSelectedFunc(func() { switchContent(1) })
	tabAbout.SetSelectedFunc(func() { switchContent(2) })

	// 初始化
	updateTabs()
	content.AddItem(systemForm, 0, 1, true)

	// 主布局
	page := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tabs, 1, 0, true).
		AddItem(content, 0, 1, false)

	settingsPages.AddPage("main", page, true, true)

	// 页面构建完成，此后的控件回调均为真实用户操作，允许保存。
	pageReady = true

	return settingsPages
}
