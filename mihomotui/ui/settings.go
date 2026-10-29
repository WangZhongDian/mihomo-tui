package ui

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

type settingItem struct {
	tview.FormItem
	id string
}

// inputBinding 设置页输入框绑定：apply 解析并应用输入到配置（返回错误表示输入无效），
// 用于在表单里实现"Tun排除地址: [管理地址列表]"这种布局。
type labeledButton struct {
	*tview.Box
	label      string
	btn        *tview.Button
	finished   func(tcell.Key)
	disabled   bool
	labelWidth int
	labelColor tcell.Color
}

func newLabeledButton(label, btnLabel string, selected func()) *labeledButton {
	lb := &labeledButton{
		Box:   tview.NewBox(),
		label: label,
		btn:   tview.NewButton(btnLabel),
	}
	lb.btn.SetSelectedFunc(selected)
	lb.Box.Primitive = lb
	return lb
}

// --- tview.FormItem 接口实现 ---
func (lb *labeledButton) GetLabel() string { return lb.label }
func (lb *labeledButton) SetFormAttributes(labelWidth int, labelColor, bgColor, fieldTextColor, fieldBgColor tcell.Color) tview.FormItem {
	lb.labelWidth = labelWidth
	lb.labelColor = labelColor
	lb.Box.SetBackgroundColor(bgColor)
	return lb
}
func (lb *labeledButton) GetFieldWidth() int  { return 0 }
func (lb *labeledButton) GetFieldHeight() int { return 1 }
func (lb *labeledButton) SetFinishedFunc(handler func(tcell.Key)) tview.FormItem {
	lb.finished = handler
	return lb
}
func (lb *labeledButton) SetDisabled(disabled bool) tview.FormItem {
	lb.disabled = disabled
	lb.btn.SetDisabled(disabled)
	return lb
}
func (lb *labeledButton) GetDisabled() bool { return lb.disabled }

// --- tview.Primitive 接口实现 ---
func (lb *labeledButton) Draw(screen tcell.Screen) {
	lb.Box.DrawForSubclass(screen, lb)
	x, y, w, _ := lb.Box.GetInnerRect()
	// 用和其他表单项相同的 labelWidth 和颜色画标签
	labelW := lb.labelWidth
	if labelW > w {
		labelW = w
	}
	tview.Print(screen, lb.label, x, y, labelW, tview.AlignLeft, lb.labelColor)
	// 把按钮放在标签右边，宽度自适应文字
	btnW := tview.TaggedStringWidth(lb.btn.GetLabel()) + 4 // 文字宽度 + 内边距
	if btnW > w-labelW {
		btnW = w - labelW
	}
	if btnW < 1 {
		btnW = 1
	}
	lb.btn.SetRect(x+labelW, y, btnW, 1)
	lb.btn.Draw(screen)
}
func (lb *labeledButton) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	// 直接把事件转发给内部按钮，Button.InputHandler 会处理 Enter（触发选中）
	// 和 Tab/Esc（离开表单）。
	return lb.btn.InputHandler()
}
func (lb *labeledButton) Focus(delegate func(p tview.Primitive)) {
	lb.Box.Focus(delegate)
	lb.btn.Focus(delegate)
}
func (lb *labeledButton) HasFocus() bool { return lb.Box.HasFocus() || lb.btn.HasFocus() }
func (lb *labeledButton) Blur() {
	lb.Box.Blur()
	lb.btn.Blur()
}
func (lb *labeledButton) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return lb.btn.MouseHandler()
}
func (lb *labeledButton) PasteHandler() func(string, func(tview.Primitive)) {
	return lb.btn.PasteHandler()
}

// buildProgressBar 生成文本进度条，percent 为 0-100
func buildProgressBar(percent int) string {
	width := 20
	filled := min(percent*width/100, width)
	return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
}

const (
	fieldAutoStart      = "system.auto_start"
	fieldSystemProxy    = "system.system_proxy"
	fieldTUN            = "system.tun"
	fieldAppLogLevel    = "app.log_level"
	fieldLogDir         = "app.log_dir"
	fieldHTTPPort       = "mihomo.http_port"
	fieldSOCKSPort      = "mihomo.socks_port"
	fieldMixedPort      = "mihomo.mixed_port"
	fieldRedirPort      = "mihomo.redir_port"
	fieldTProxyPort     = "mihomo.tproxy_port"
	fieldAllowLAN       = "mihomo.allow_lan"
	fieldIPv6           = "mihomo.ipv6"
	fieldUnifiedDelay   = "mihomo.unified_delay"
	fieldAutoRedirect   = "mihomo.auto_redirect"
	fieldMihomoLogLevel = "mihomo.log_level"
	fieldDefaultPolicy  = "app.default_proxy_group"
	fieldTestURL        = "mihomo.test_url"
)

// inputBinding 设置页输入框绑定：apply 解析并应用输入到配置（返回错误表示输入无效），
// current 读取当前生效值的显示形式（用于内容比对与失败后恢复显示）。
type inputBinding struct {
	apply   func(fresh *mihomotui.Config, text string) error
	current func(c *mihomotui.Config) string
}

// isProxyPortField 判断设置项是否为系统代理会用到的端口字段。
// 这些端口变化后必须重建 shell 注入块，否则代理会指向旧端口。
func isProxyPortField(label string) bool {
	switch label {
	case fieldMixedPort, fieldHTTPPort, fieldSOCKSPort:
		return true
	default:
		return false
	}
}

// NewSettingsPage 创建设置页面
func NewSettingsPage(app *tview.Application) tview.Primitive {
	activeTab := 0 // 0=系统设置, 1=mihomo设置, 2=关于

	// Tab 按钮
	tabSystem := tview.NewButton(" " + T("settings.system") + " ")
	tabSystem.SetBorder(false)
	tabMihomo := tview.NewButton(" " + T("settings.mihomo") + " ")
	tabMihomo.SetBorder(false)
	tabAbout := tview.NewButton(" " + T("settings.about") + " ")
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
			AddButtons([]string{T("common.confirm")}).
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
					showModal(T("settings.save_failed"), fmt.Sprintf("%s: %v", fieldName, err))
				})
				return
			}
			if !resp.Applied {
				mihomotui.Warnf("设置 %s 已保存，但应用失败（%s）: %s", fieldName, resp.ApplyStage, resp.ApplyError)
			}
			// 本地缓存已在提交成功后同步，以其为准刷新日志配置
			g := mihomotui.GlobalConfig()
			_ = mihomotui.InitLogger(g.LogDir, g.LogLevel)
			// 代理端口变化后，已注入的 shell 块仍指向旧端口，必须按新端口重建。
			if isProxyPortField(fieldName) {
				if _, rerr := mihomotui.ReconcileSystemProxyEnv(); rerr != nil {
					mihomotui.Warnf("端口变更后重建系统代理环境变量失败: %v", rerr)
				}
			}
			mihomotui.Infof("设置 %s 已保存", fieldName)
		}()
	}

	// 按稳定字段 ID 查找表单控件，显示标签可以安全翻译。
	findCheckbox := func(form *tview.Form, id string) *tview.Checkbox {
		for i := 0; i < form.GetFormItemCount(); i++ {
			if item, ok := form.GetFormItem(i).(*settingItem); ok && item.id == id {
				if cb, ok := item.FormItem.(*tview.Checkbox); ok {
					return cb
				}
			}
		}
		return nil
	}
	findDropDown := func(form *tview.Form, id string) *tview.DropDown {
		for i := 0; i < form.GetFormItemCount(); i++ {
			if item, ok := form.GetFormItem(i).(*settingItem); ok && item.id == id {
				if dd, ok := item.FormItem.(*tview.DropDown); ok {
					return dd
				}
			}
		}
		return nil
	}

	// systemForm 稍后赋值；排除地址管理弹窗关闭时需把焦点还回去。
	var systemForm *tview.Form
	// openExcludeAddressManager 在后面（pageReady = true 之后）赋值，
	// 避免函数体引用尚未声明的局部变量。
	var openExcludeAddressManager func()
	var openFakeIPFilterManager func()

	// bindInputSave 为表单输入框设置失焦保存：仅在内容变化时提交；
	// 输入无法解析或保存失败时恢复显示为当前生效值，避免界面与实际配置不一致。
	bindInputSave := func(form *tview.Form, bindings map[string]inputBinding) {
		for i := 0; i < form.GetFormItemCount(); i++ {
			item, ok := form.GetFormItem(i).(*settingItem)
			if !ok {
				continue
			}
			field, ok := item.FormItem.(*tview.InputField)
			if !ok {
				continue
			}
			binding, ok := bindings[item.id]
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
					showModal(T("settings.invalid_input"), fmt.Sprintf("%s: %v", inputField.GetLabel(), err))
					return
				}
				doSave(item.id,
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
					return fmt.Errorf("%s", T("settings.invalid_number"))
				}
				set(fresh, v)
				return nil
			},
			current: func(c *mihomotui.Config) string { return strconv.Itoa(get(c)) },
		}
	}

	languagePreferenceIndex := func() int {
		switch mihomotui.ClientPrefs().Language {
		case "zh-CN":
			return 1
		case "en":
			return 2
		default:
			return 0
		}
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
	systemForm = tview.NewForm()
	addSetting := func(form *tview.Form, id string, item tview.FormItem) *tview.Form {
		return form.AddFormItem(&settingItem{FormItem: item, id: id})
	}
	addSetting(systemForm, fieldAutoStart, tview.NewCheckbox().SetLabel(T("settings.autostart")).SetChecked(cfg.System.AutoStart).SetChangedFunc(func(checked bool) {
		doSave(fieldAutoStart,
			func(fresh *mihomotui.Config) { fresh.System.AutoStart = checked },
			func() {
				if cb := findCheckbox(systemForm, fieldAutoStart); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().System.AutoStart) })
				}
			})
	}))
	addSetting(systemForm, fieldSystemProxy, tview.NewCheckbox().SetLabel(T("settings.system_proxy")).SetChecked(mihomotui.GetSystemProxyPreference()).SetChangedFunc(func(checked bool) {
		if reverting || !pageReady {
			return
		}
		// 系统代理是当前 TUI 用户的本地偏好：写本用户环境变量 + 本地偏好文件，
		// 不写入 daemon 全局配置。端口自检、会话注入与结果提示统一由
		// toggleSystemProxy 处理；这里只负责失败时把勾选状态回滚。
		toggleSystemProxy(app, settingsPages, checked, func() {
			if mihomotui.GetSystemProxyPreference() == checked {
				return
			}
			if cb := findCheckbox(systemForm, fieldSystemProxy); cb != nil {
				silentSet(func() { cb.SetChecked(!checked) })
			}
		})
	}))
	systemForm.AddFormItem(newLabeledButton(T("settings.tun_exclusions"), T("settings.manage_addresses"), func() {
		if openExcludeAddressManager != nil {
			openExcludeAddressManager()
		}
	}))
	systemForm.AddFormItem(newLabeledButton(T("settings.fakeip_filter"), T("settings.manage_domains"), func() {
		if openFakeIPFilterManager != nil {
			openFakeIPFilterManager()
		}
	}))
	addSetting(systemForm, fieldTUN, tview.NewCheckbox().SetLabel(T("settings.tun")).SetChecked(cfg.System.TUN).SetChangedFunc(func(checked bool) {
		doSave(fieldTUN,
			func(fresh *mihomotui.Config) { fresh.System.TUN = checked },
			func() {
				if cb := findCheckbox(systemForm, fieldTUN); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().System.TUN) })
				}
			})
	}))
	addSetting(systemForm, "ui.language", tview.NewDropDown().SetLabel(T("settings.language")).SetOptions([]string{T("settings.language.follow_system"), "简体中文", "English"}, func(_ string, optionIndex int) {
		if !pageReady || reverting {
			return
		}
		var err error
		switch optionIndex {
		case 1:
			err = mihomotui.SetLanguagePreference("zh-CN")
		case 2:
			err = mihomotui.SetLanguagePreference("en")
		default:
			err = mihomotui.SetFollowSystemPreference()
		}
		if err != nil {
			if dd := findDropDown(systemForm, "ui.language"); dd != nil {
				silentSet(func() { dd.SetCurrentOption(languagePreferenceIndex()) })
			}
			showModal(T("settings.language.save_failed"), err.Error())
			return
		}
		ShowAlertModal(app, settingsPages, T("settings.language"), T("settings.language.restart"))
	}).SetCurrentOption(languagePreferenceIndex()))
	addSetting(systemForm, fieldAppLogLevel, tview.NewDropDown().SetLabel(T("settings.log_level")).SetOptions([]string{"DEBUG", "INFO", "WARN", "ERROR"}, func(option string, optionIndex int) {
		level := appLogLevels[optionIndex]
		if level == mihomotui.GlobalConfig().LogLevel {
			return
		}
		doSave(fieldAppLogLevel,
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
				if dd := findDropDown(systemForm, fieldAppLogLevel); dd != nil {
					silentSet(func() { dd.SetCurrentOption(idx) })
				}
			})
	}).SetCurrentOption(appLogIdx))
	addSetting(systemForm, fieldLogDir, tview.NewInputField().SetLabel(T("settings.log_dir")).SetText(cfg.LogDir).SetFieldWidth(50))
	systemForm.AddInputField(T("settings.work_dir"), workDir, 50, func(text string, ch rune) bool { return false }, nil)

	// 系统设置输入框失焦保存（工作目录为只读字段，不在绑定表中）
	bindInputSave(systemForm, map[string]inputBinding{
		fieldLogDir: {
			apply: func(fresh *mihomotui.Config, text string) error {
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("%s", T("settings.log_dir_required"))
				}
				fresh.LogDir = text
				return nil
			},
			current: func(c *mihomotui.Config) string { return c.LogDir },
		},
	})
	systemForm.SetBorder(true).SetTitle(" " + T("settings.system") + " ")

	// ---------- mihomo 设置 ----------
	mihomoForm := tview.NewForm()
	for _, item := range []struct {
		id, label, value string
	}{
		{fieldHTTPPort, T("settings.http_port"), strconv.Itoa(cfg.Mihomo.HTTPPort)},
		{fieldSOCKSPort, T("settings.socks_port"), strconv.Itoa(cfg.Mihomo.SOCKS5Port)},
		{fieldMixedPort, T("settings.mixed_port"), strconv.Itoa(cfg.Mihomo.MixedPort)},
		{fieldRedirPort, T("settings.redir_port"), strconv.Itoa(cfg.Mihomo.RedirPort)},
		{fieldTProxyPort, T("settings.tproxy_port"), strconv.Itoa(cfg.Mihomo.TProxyPort)},
	} {
		addSetting(mihomoForm, item.id, tview.NewInputField().SetLabel(item.label).SetText(item.value).SetFieldWidth(10))
	}
	addSetting(mihomoForm, fieldAllowLAN, tview.NewCheckbox().SetLabel(T("settings.allow_lan")).SetChecked(cfg.Mihomo.AllowLan).SetChangedFunc(func(checked bool) {
		doSave(fieldAllowLAN,
			func(fresh *mihomotui.Config) { fresh.Mihomo.AllowLan = checked },
			func() {
				if cb := findCheckbox(mihomoForm, fieldAllowLAN); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.AllowLan) })
				}
			})
	}))
	addSetting(mihomoForm, fieldIPv6, tview.NewCheckbox().SetLabel("IPv6").SetChecked(cfg.Mihomo.IPv6).SetChangedFunc(func(checked bool) {
		doSave(fieldIPv6,
			func(fresh *mihomotui.Config) { fresh.Mihomo.IPv6 = checked },
			func() {
				if cb := findCheckbox(mihomoForm, fieldIPv6); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.IPv6) })
				}
			})
	}))
	addSetting(mihomoForm, fieldUnifiedDelay, tview.NewCheckbox().SetLabel(T("settings.unified_delay")).SetChecked(cfg.Mihomo.UnifiedDelay).SetChangedFunc(func(checked bool) {
		doSave(fieldUnifiedDelay,
			func(fresh *mihomotui.Config) { fresh.Mihomo.UnifiedDelay = checked },
			func() {
				if cb := findCheckbox(mihomoForm, fieldUnifiedDelay); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.UnifiedDelay) })
				}
			})
	}))
	addSetting(mihomoForm, fieldAutoRedirect, tview.NewCheckbox().SetLabel(T("settings.auto_redirect")).SetChecked(cfg.Mihomo.AutoRedirect).SetChangedFunc(func(checked bool) {
		doSave(fieldAutoRedirect,
			func(fresh *mihomotui.Config) { fresh.Mihomo.AutoRedirect = checked },
			func() {
				if cb := findCheckbox(mihomoForm, fieldAutoRedirect); cb != nil {
					silentSet(func() { cb.SetChecked(mihomotui.GlobalConfig().Mihomo.AutoRedirect) })
				}
			})
	}))
	addSetting(mihomoForm, fieldMihomoLogLevel, tview.NewDropDown().SetLabel(T("settings.mihomo_log_level")).SetOptions([]string{"DEBUG", "INFO", "WARNING", "ERROR", "SILENT"}, func(option string, optionIndex int) {
		level := logLevels[optionIndex]
		if level == mihomotui.GlobalConfig().Mihomo.LogLevel {
			return
		}
		doSave(fieldMihomoLogLevel,
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
				if dd := findDropDown(mihomoForm, fieldMihomoLogLevel); dd != nil {
					silentSet(func() { dd.SetCurrentOption(idx) })
				}
			})
	}))
	addSetting(mihomoForm, fieldDefaultPolicy, tview.NewDropDown().SetLabel(T("settings.default_policy")).SetOptions(mihomotui.PolicyList, func(option string, optionIndex int) {
		if option == mihomotui.GlobalConfig().DefaultProxyGroup {
			return
		}
		doSave(fieldDefaultPolicy,
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
				if dd := findDropDown(mihomoForm, fieldDefaultPolicy); dd != nil {
					silentSet(func() { dd.SetCurrentOption(idx) })
				}
			})
	}))
	addSetting(mihomoForm, fieldTestURL, tview.NewInputField().SetLabel(T("settings.test_url")).SetText(cfg.Mihomo.TestURL).SetFieldWidth(50))

	// mihomo 设置输入框失焦保存
	bindInputSave(mihomoForm, map[string]inputBinding{
		fieldHTTPPort: portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.HTTPPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.HTTPPort },
		),
		fieldSOCKSPort: portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.SOCKS5Port = v },
			func(c *mihomotui.Config) int { return c.Mihomo.SOCKS5Port },
		),
		fieldMixedPort: portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.MixedPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.MixedPort },
		),
		fieldRedirPort: portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.RedirPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.RedirPort },
		),
		fieldTProxyPort: portBinding(
			func(fresh *mihomotui.Config, v int) { fresh.Mihomo.TProxyPort = v },
			func(c *mihomotui.Config) int { return c.Mihomo.TProxyPort },
		),
		fieldTestURL: {
			apply: func(fresh *mihomotui.Config, text string) error {
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("%s", T("settings.test_url_required"))
				}
				fresh.Mihomo.TestURL = text
				return nil
			},
			current: func(c *mihomotui.Config) string { return c.Mihomo.TestURL },
		},
	})
	mihomoForm.SetBorder(true).SetTitle(" " + T("settings.mihomo") + " ")

	// 内核与外部资源管理已移至左侧“资源管理”页面；此处仅保留 mihomo 配置表单。
	mihomoContent := mihomoForm

	// ---------- 关于页面 ----------
	aboutText := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText("\n\n[::b]mihomo-tui[-:-:-]\n\n" +
			Tf("settings.about.version", map[string]any{"Version": mihomotui.Version}) + "\n" +
			Tf("settings.about.go_version", map[string]any{"Version": runtime.Version()}) + "\n\n" +
			T("settings.about.description") + "\n\n" +
			T("settings.about.repository") + "\n\n[" + mihomotui.ColorMuted + "]© 2025 youmetme[-]")
	aboutText.SetBorder(true).SetTitle(" " + T("settings.about") + " ")

	shutdownBtn := tview.NewButton(" " + T("settings.shutdown") + " ")
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
			tabSystem.SetLabel("[black:blue:b] " + T("settings.system") + " [-:-:-]")
		} else {
			tabSystem.SetLabel(" " + T("settings.system") + " ")
		}
		if activeTab == 1 {
			tabMihomo.SetLabel("[black:blue:b] " + T("settings.mihomo") + " [-:-:-]")
		} else {
			tabMihomo.SetLabel(" " + T("settings.mihomo") + " ")
		}
		if activeTab == 2 {
			tabAbout.SetLabel("[black:blue:b] " + T("settings.about") + " [-:-:-]")
		} else {
			tabAbout.SetLabel(" " + T("settings.about") + " ")
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

	// ==================== 排除地址管理弹窗定义 ====================
	// openExcludeAddressManager 打开排除地址管理弹窗：支持增删 IP/CIDR/域名。
	openExcludeAddressManager = func() {
		var input *tview.InputField
		ShowOverlay(app, settingsPages, "exclude-address-manager", T("settings.exclude_manager"), func(close func()) (tview.Primitive, tview.Primitive) {
			input = tview.NewInputField().
				SetLabel(T("settings.new_address")).
				SetFieldWidth(40).
				SetPlaceholder(T("settings.address_placeholder"))
			input.SetBorder(true).SetTitle(" " + T("settings.input") + " ")

			addBtn := tview.NewButton(" " + T("common.add") + " ")
			closeBtn := tview.NewButton(" " + T("common.close") + " ")
			hint := tview.NewTextView().
				SetDynamicColors(true).
				SetText("[" + mihomotui.ColorMuted + "]" + T("settings.exclude_hint") + "[-]")

			list := tview.NewFlex().SetDirection(tview.FlexRow)
			list.SetBorder(true).SetTitle(" " + T("settings.exclude_list") + " ")

			var refreshList func()
			commitAddrs := func(mutate func(addrs []string) []string) {
				go func() {
					resp, err := mihomotui.MutateServerConfig(func(fresh *mihomotui.Config) {
						fresh.ExcludeAddresses = mutate(append([]string(nil), fresh.ExcludeAddresses...))
					})
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal(T("settings.save_failed"), err.Error())
							return
						}
						if resp != nil && !resp.Applied {
							mihomotui.Warnf("排除地址已保存，但应用失败（%s）: %s", resp.ApplyStage, resp.ApplyError)
						}
						refreshList()
					})
				}()
			}

			refreshList = func() {
				addrs := mihomotui.GlobalConfig().ExcludeAddresses
				list.Clear()
				if len(addrs) == 0 {
					list.AddItem(tview.NewTextView().SetText(T("settings.exclude_empty")), 1, 0, false)
					return
				}
				for i, addr := range addrs {
					idx, entry := i, addr
					info := tview.NewTextView().SetText(fmt.Sprintf(" %d. %s", idx+1, entry))
					del := tview.NewButton(" " + T("common.delete") + " ")
					del.SetSelectedFunc(func() {
						ShowConfirmModal(app, settingsPages, T("settings.confirm_delete"), entry, func() {
							commitAddrs(func(cur []string) []string {
								out := make([]string, 0, len(cur))
								for _, v := range cur {
									if v != entry {
										out = append(out, v)
									}
								}
								return out
							})
						})
					})
					row := tview.NewFlex().
						AddItem(info, 0, 1, false).
						AddItem(del, 10, 0, false)
					list.AddItem(row, 1, 0, false)
				}
			}

			doAdd := func() {
				raw := strings.TrimSpace(input.GetText())
				if raw == "" {
					showModal(T("settings.add_failed"), T("settings.address_required"))
					return
				}
				entries := mihomotui.ParseExcludeAddresses(raw)
				if len(entries) == 0 {
					showModal(T("settings.add_failed"), T("settings.address_invalid"))
					return
				}
				for _, entry := range entries {
					if !mihomotui.IsValidExcludeAddress(entry) {
						showModal(T("settings.add_failed"), fmt.Sprintf(T("settings.address_format")+": %s", entry))
						return
					}
				}
				commitAddrs(func(cur []string) []string {
					seen := map[string]bool{}
					for _, v := range cur {
						seen[v] = true
					}
					for _, entry := range entries {
						if !seen[entry] {
							cur = append(cur, entry)
							seen[entry] = true
						}
					}
					return cur
				})
				input.SetText("")
			}
			addBtn.SetSelectedFunc(doAdd)
			input.SetDoneFunc(func(key tcell.Key) {
				if key == tcell.KeyEnter {
					doAdd()
				}
			})

			closeBtn.SetSelectedFunc(close)

			topBar := tview.NewFlex().
				AddItem(input, 0, 1, true).
				AddItem(addBtn, 10, 0, false)
			bottomBar := tview.NewFlex().
				AddItem(hint, 0, 1, false).
				AddItem(closeBtn, 10, 0, false)
			body := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(topBar, 3, 0, true).
				AddItem(list, 0, 1, false).
				AddItem(bottomBar, 1, 0, false)

			refreshList()
			return body, input
		})
	}

	// ==================== Fake-IP 过滤管理弹窗 ====================
	// 命中 fake-ip-filter 的域名返回真实 IP（不走 fake-ip），用于组网/STUN 等
	// 需要真实出口 IP 的场景。内置默认仅 *.lan 与 localhost.ptlogin2.qq.com，
	// 此处管理用户追加的自定义域名。
	openFakeIPFilterManager = func() {
		var input *tview.InputField
		ShowOverlay(app, settingsPages, "fakeip-filter-manager", T("settings.fakeip_manager"), func(close func()) (tview.Primitive, tview.Primitive) {
			input = tview.NewInputField().
				SetLabel(T("settings.new_domain")).
				SetFieldWidth(40).
				SetPlaceholder(T("settings.domain_placeholder"))
			input.SetBorder(true).SetTitle(" " + T("settings.input") + " ")

			addBtn := tview.NewButton(" " + T("common.add") + " ")
			closeBtn := tview.NewButton(" " + T("common.close") + " ")
			hint := tview.NewTextView().
				SetDynamicColors(true).
				SetText("[" + mihomotui.ColorMuted + "]" + T("settings.fakeip_hint") + "[-]")

			list := tview.NewFlex().SetDirection(tview.FlexRow)
			list.SetBorder(true).SetTitle(" " + T("settings.fakeip_list") + " ")

			var refreshList func()
			commitFilter := func(mutate func(addrs []string) []string) {
				go func() {
					resp, err := mihomotui.MutateServerConfig(func(fresh *mihomotui.Config) {
						fresh.FakeIPFilter = mutate(append([]string(nil), fresh.FakeIPFilter...))
					})
					app.QueueUpdateDraw(func() {
						if err != nil {
							showModal(T("settings.save_failed"), err.Error())
							return
						}
						if resp != nil && !resp.Applied {
							mihomotui.Warnf("fake-ip-filter 已保存，但应用失败（%s）: %s", resp.ApplyStage, resp.ApplyError)
						}
						refreshList()
					})
				}()
			}

			refreshList = func() {
				addrs := mihomotui.GlobalConfig().FakeIPFilter
				list.Clear()
				if len(addrs) == 0 {
					list.AddItem(tview.NewTextView().SetText(T("settings.fakeip_empty")), 1, 0, false)
					return
				}
				for i, addr := range addrs {
					idx, entry := i, addr
					info := tview.NewTextView().SetText(fmt.Sprintf(" %d. %s", idx+1, entry))
					del := tview.NewButton(" " + T("common.delete") + " ")
					del.SetSelectedFunc(func() {
						ShowConfirmModal(app, settingsPages, T("settings.confirm_delete"), entry, func() {
							commitFilter(func(cur []string) []string {
								out := make([]string, 0, len(cur))
								for _, v := range cur {
									if v != entry {
										out = append(out, v)
									}
								}
								return out
							})
						})
					})
					row := tview.NewFlex().
						AddItem(info, 0, 1, false).
						AddItem(del, 10, 0, false)
					list.AddItem(row, 1, 0, false)
				}
			}

			doAdd := func() {
				raw := strings.TrimSpace(input.GetText())
				if raw == "" {
					showModal(T("settings.add_failed"), T("settings.domain_required"))
					return
				}
				entries := mihomotui.ParseExcludeAddresses(raw) // 复用同一分隔规则（换行/逗号/分号）
				if len(entries) == 0 {
					showModal(T("settings.add_failed"), T("settings.domain_invalid"))
					return
				}
				for _, entry := range entries {
					if !mihomotui.IsValidFakeIPFilterEntry(entry) {
						showModal(T("settings.add_failed"), fmt.Sprintf(T("settings.domain_format")+": %s", entry))
						return
					}
				}
				commitFilter(func(cur []string) []string {
					seen := map[string]bool{}
					for _, v := range cur {
						seen[v] = true
					}
					for _, entry := range entries {
						if !seen[entry] {
							cur = append(cur, entry)
							seen[entry] = true
						}
					}
					return cur
				})
				input.SetText("")
			}
			addBtn.SetSelectedFunc(doAdd)
			input.SetDoneFunc(func(key tcell.Key) {
				if key == tcell.KeyEnter {
					doAdd()
				}
			})

			closeBtn.SetSelectedFunc(close)

			topBar := tview.NewFlex().
				AddItem(input, 0, 1, true).
				AddItem(addBtn, 10, 0, false)
			bottomBar := tview.NewFlex().
				AddItem(hint, 0, 1, false).
				AddItem(closeBtn, 10, 0, false)
			body := tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(topBar, 3, 0, true).
				AddItem(list, 0, 1, false).
				AddItem(bottomBar, 1, 0, false)

			refreshList()
			return body, input
		})
	}
	// =================================================================
	return settingsPages
}
