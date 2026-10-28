package ui

import (
	"fmt"

	"github.com/rivo/tview"
	"mihomotui/mihomotui"
)

// toggleSystemProxy 统一处理「系统代理」开关（首页网络设置卡片与设置页共用）：
//
//  1. 开启前检查本地代理端口是否在监听——内核没启动时注入代理，会让所有
//     走 http_proxy 的程序连接失败，所以先确认再让用户决定；
//  2. 幂等写入 shell 配置块；
//  3. 同步当前会话（systemctl --user set-environment / dbus-update-activation-environment），
//     让新开的终端与桌面应用无需 source 即可生效；
//  4. 保存本用户的 system_proxy 偏好，并提示当前终端如何立即生效。
//
// onDone 在流程结束（成功、失败或用户取消）后调用，调用方据此把控件
// 刷新回真实状态。pages 用于承载弹窗，必须是当前可见的 Pages 容器。
func toggleSystemProxy(app *tview.Application, pages *tview.Pages, enable bool, onDone func()) {
	if app == nil || pages == nil {
		return
	}

	apply := func() {
		go func() {
			cfg := mihomotui.GlobalConfig()
			err := cfg.SetSystemProxyEnv(enable)
			if err == nil {
				// 会话注入是增强项：失败只告警，不回滚已写入的 shell 配置。
				if serr := mihomotui.SyncSystemProxySession(enable); serr != nil {
					mihomotui.Warnf("同步系统代理到当前会话失败: %v", serr)
				}
				if perr := mihomotui.SetSystemProxyPreference(enable); perr != nil {
					err = fmt.Errorf("环境变量已写入，但偏好保存失败: %w", perr)
				}
			}
			app.QueueUpdateDraw(func() {
				if onDone != nil {
					onDone()
				}
				if err != nil {
					mihomotui.Warnf("系统代理设置失败: %v", err)
					ShowAlertModal(app, pages, "系统代理设置失败", err.Error())
					return
				}
				if enable {
					mihomotui.Infof("系统代理已开启")
					ShowAlertModal(app, pages, "系统代理已开启", mihomotui.SystemProxySessionHint())
					return
				}
				mihomotui.Infof("系统代理已关闭")
				ShowAlertModal(app, pages, "系统代理已关闭", mihomotui.SystemProxyClearHint())
			})
		}()
	}

	if !enable {
		apply()
		return
	}

	// 开启前做端口自检：端口不通时注入只会让网络看起来"更坏"。
	cfg := mihomotui.GlobalConfig()
	httpPort, _ := mihomotui.SystemProxyPorts(cfg)
	go func() {
		alive := mihomotui.ProxyPortListening(httpPort)
		app.QueueUpdateDraw(func() {
			if alive {
				apply()
				return
			}
			ShowModal(app, pages, "代理端口未监听",
				fmt.Sprintf("本地端口 127.0.0.1:%d 当前没有监听（mihomo 内核可能未启动）。\n\n"+
					"此时开启系统代理，所有走代理的程序都会连接失败。\n"+
					"建议先在首页启动内核，再开启系统代理。\n\n仍然开启吗？", httpPort),
				[]string{"仍然开启", "取消"}, func(buttonIndex int, _ string) {
					if buttonIndex == 0 {
						apply()
						return
					}
					if onDone != nil {
						onDone()
					}
				})
		})
	}()
}
