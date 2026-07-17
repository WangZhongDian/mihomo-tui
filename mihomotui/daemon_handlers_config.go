package mihomotui

import (
	"errors"
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
		Infof("收到配置更新请求: system.tun=%v proxy_mode=%s version=%d", req.System.TUN, req.ProxyMode, req.Version)

		// 先校验客户端提交内容（提交层仍会复核，此处用于给出准确的 400 语义）。
		if err := req.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("配置校验失败: %w", err))
			return
		}

		// 原子提交：版本冲突检测 + 全字段校验 + 原子落盘 + 替换内存。
		// /config 响应会掩码 secret；客户端提交空 secret 时由提交层保留当前值。
		oldTUN := GlobalConfig().System.TUN
		committed, err := ReplaceGlobalConfig(req)
		if err != nil {
			if errors.Is(err, ErrConfigConflict) {
				writeError(w, http.StatusConflict, err)
			} else {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存配置失败: %w", err))
			}
			return
		}
		Infof("配置已提交: version=%d system.tun=%v proxy_mode=%s", committed.Version, committed.System.TUN, committed.ProxyMode)

		// 提交后串行应用运行时变更（生成 mihomo 配置 → 热重载/重启 → TUN 同步），
		// 同步等待结果，使"保存成功但应用失败"的状态对调用方可见。
		report := d.reconcileConfigChange("config", oldTUN, committed.System.TUN)

		resp := ConfigUpdateResponse{
			Config:     committed,
			Applied:    report.Applied,
			ApplyStage: report.Stage,
			ApplyError: report.Err,
		}
		resp.Config.Mihomo.Secret = ""
		writeJSON(w, http.StatusOK, ok(resp))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
