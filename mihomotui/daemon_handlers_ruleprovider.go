package mihomotui

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (d *Daemon) handleRuleProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.RuleProviderSubscriptions))
	case http.MethodPost:
		var req RuleProviderImportRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("规则订阅链接不能为空"))
			return
		}
		name := req.Name
		if name == "" {
			name = "规则订阅"
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		// 生成唯一名称
		baseName := name
		for i := 1; i <= 100; i++ {
			if cfg.FindRuleProviderByName(name) < 0 {
				break
			}
			name = fmt.Sprintf("%s%d", baseName, i)
		}
		behavior := req.Behavior
		if behavior == "" {
			behavior = "classical"
		}
		format := req.Format
		if format == "" {
			format = "yaml"
		}
		interval := req.Interval
		if interval <= 0 {
			interval = DayInSeconds
		}
		proxyGroup := req.ProxyGroup
		if proxyGroup == "" {
			proxyGroup = "Auto"
		}
		cfg.RuleProviderSubscriptions = append(cfg.RuleProviderSubscriptions, RuleProviderSubscription{
			Name:       name,
			URL:        req.URL,
			Behavior:   behavior,
			Format:     format,
			Interval:   interval,
			ProxyGroup: proxyGroup,
			UpdatedAt:  time.Now().Format(TimeFormatShort),
		})
		if err := cfg.Flush(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存规则订阅失败: %w", err))
			return
		}
		Infof("规则订阅导入成功: name=%s url=%s behavior=%s", name, req.URL, behavior)
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

func (d *Daemon) handleRuleProviderDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/rule-providers/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("缺少规则订阅名称"))
		return
	}
	name := parts[0]

	switch r.Method {
	case http.MethodPut:
		// 刷新规则订阅：更新 UpdatedAt
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		idx := cfg.FindRuleProviderByName(name)
		if idx < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("规则订阅不存在: %s", name))
			return
		}
		cfg.RuleProviderSubscriptions[idx].UpdatedAt = time.Now().Format(TimeFormatShort)
		if err := cfg.Flush(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("保存失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok("规则订阅刷新已接收"))
	case http.MethodDelete:
		// 删除规则订阅
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := GlobalConfig()
		newRps := make([]RuleProviderSubscription, 0, len(cfg.RuleProviderSubscriptions))
		for _, rp := range cfg.RuleProviderSubscriptions {
			if rp.Name != name {
				newRps = append(newRps, rp)
			}
		}
		cfg.RuleProviderSubscriptions = newRps
		if err := cfg.Flush(); err != nil {
			Warnf("删除规则订阅后保存配置失败: %v", err)
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
