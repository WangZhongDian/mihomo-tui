package mihomotui

import (
	"fmt"
	"net/http"
)

func (d *Daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Secret 不跟随常规配置响应发送；需要连接 mihomo API 的授权客户端
		// 必须通过受 IPC peer credential 保护的专用端点获取。
		cfg := *GlobalConfig()
		cfg.Mihomo.Secret = ""
		writeJSON(w, http.StatusOK, ok(ConfigResponse{Config: cfg}))
	case http.MethodPost:
		var req Config
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析配置失败: %w", err))
			return
		}
		// /config 不再返回 secret。客户端若提交被掩码的配置，必须保留服务端已有 secret，
		// 否则会导致运行中的 mihomo API 鉴权失效。
		current := GlobalConfig()
		if req.Mihomo.Secret == "" {
			req.Mihomo.Secret = current.Mihomo.Secret
		}
		Infof("收到配置更新请求: system.tun=%v proxy_mode=%s", req.System.TUN, req.ProxyMode)
		oldTUN := current.System.TUN
		d.mu.Lock()
		SetGlobalConfig(req)
		cfg := GlobalConfig()
		if err := cfg.Flush(); err != nil {
			d.mu.Unlock()
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存配置失败: %w", err))
			return
		}
		newTUN := cfg.System.TUN
		Infof("配置已保存到文件: system.tun=%v proxy_mode=%s", cfg.System.TUN, cfg.ProxyMode)
		d.mu.Unlock()

		// 配置变更后自动重新生成 mihomo 配置并热重载
		go func() {
			cfg := GlobalConfig()
			if err := cfg.GenerateMihomoConfig(); err != nil {
				Errorf("配置变更后重新生成 mihomo 配置失败: %v", err)
				return
			}
			Infof("配置变更，已重新生成 mihomo 配置")

			// 若 mihomo 正在运行，尝试热重载
			if d.mihomoProcess != nil && d.mihomoProcess.IsRunning() {
				if d.mihomoAPI != nil {
					if err := d.mihomoAPI.ReloadConfigs(true); err != nil {
						Warnf("热重载 mihomo 失败: %v，尝试重启", err)
						if err := d.mihomoProcess.Restart(); err != nil {
							Errorf("重启 mihomo 失败: %v", err)
						}
					} else {
						Infof("mihomo 配置已热重载")
						// 热重载成功且 TUN 状态发生变化时，同步设置/清理路由修复规则
						if oldTUN != newTUN {
							if newTUN {
								if err := SetupTUNRouting(); err != nil {
									Warnf("TUN 路由修复设置失败（外部入站连接可能受影响）: %v", err)
								}
							} else {
								if err := RestoreTUNRouting(); err != nil {
									Warnf("TUN 路由规则清理失败: %v", err)
								}
							}
						}
					}
				}
			}
		}()

		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
