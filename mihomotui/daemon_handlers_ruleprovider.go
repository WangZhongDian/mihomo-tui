package mihomotui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		if err := d.importRuleProvider(req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("导入规则订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

func (d *Daemon) importRuleProvider(req RuleProviderImportRequest) error {
	if _, err := fetchRuleProvider(req.URL); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = deriveSubscriptionName(req.URL)
	}
	behavior, err := normalizeRuleProviderBehavior(req.Behavior)
	if err != nil {
		return err
	}
	format, err := normalizeRuleProviderFormat(req.Format)
	if err != nil {
		return err
	}
	interval := req.Interval
	if interval <= 0 {
		interval = DayInSeconds
	}
	proxyGroup := strings.TrimSpace(req.ProxyGroup)
	if proxyGroup == "" {
		proxyGroup = "Auto"
	}

	// 原子提交规则订阅变更。同名规则订阅表示更新，避免重复导入后
	// 产生不稳定的“规则订阅2”等名称。
	var shouldApply bool
	_, err = UpdateGlobalConfig(func(cfg *Config) error {
		idx := cfg.FindRuleProviderByName(name)
		if idx < 0 {
			name = uniqueRuleProviderName(name, cfg)
		}
		now := time.Now().Format(TimeFormatShort)
		if idx >= 0 {
			cfg.RuleProviderSubscriptions[idx].URL = req.URL
			cfg.RuleProviderSubscriptions[idx].Behavior = behavior
			cfg.RuleProviderSubscriptions[idx].Format = format
			cfg.RuleProviderSubscriptions[idx].Interval = interval
			cfg.RuleProviderSubscriptions[idx].ProxyGroup = proxyGroup
			cfg.RuleProviderSubscriptions[idx].UpdatedAt = now
			cfg.RuleProviderSubscriptions[idx].LastSuccessAt = now
			cfg.RuleProviderSubscriptions[idx].LastFailureAt = ""
			cfg.RuleProviderSubscriptions[idx].LastError = ""
		} else {
			cfg.RuleProviderSubscriptions = append(cfg.RuleProviderSubscriptions, RuleProviderSubscription{
				Name:          name,
				URL:           req.URL,
				Behavior:      behavior,
				Format:        format,
				Interval:      interval,
				ProxyGroup:    proxyGroup,
				UpdatedAt:     now,
				LastSuccessAt: now,
			})
		}
		shouldApply = hasUsableProxySubscription(cfg)
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存规则订阅失败: %w", err)
	}
	if shouldApply {
		if report := d.reconcileLatest("ruleprovider-import"); !report.Applied {
			return fmt.Errorf("规则订阅导入成功，但应用新配置失败: %s", report.Err)
		}
	}
	Infof("规则订阅导入成功: name=%s url=%s behavior=%s", name, RedactURL(req.URL), behavior)
	return nil
}

func (d *Daemon) refreshRuleProvider(name string) error {
	cfg := GlobalConfig()
	idx := cfg.FindRuleProviderByName(name)
	if idx < 0 {
		return fmt.Errorf("规则订阅不存在: %s", name)
	}
	rp := cfg.RuleProviderSubscriptions[idx]

	_, fetchErr := fetchRuleProvider(rp.URL)
	var shouldApply bool
	_, err := UpdateGlobalConfig(func(cfg *Config) error {
		idx := cfg.FindRuleProviderByName(name)
		if idx < 0 {
			return fmt.Errorf("规则订阅在刷新期间已被删除: %s", name)
		}
		now := time.Now().Format(TimeFormatShort)
		if fetchErr != nil {
			cfg.RuleProviderSubscriptions[idx].LastError = fetchErr.Error()
			cfg.RuleProviderSubscriptions[idx].LastFailureAt = now
			return nil
		}
		cfg.RuleProviderSubscriptions[idx].UpdatedAt = now
		cfg.RuleProviderSubscriptions[idx].LastSuccessAt = now
		cfg.RuleProviderSubscriptions[idx].LastError = ""
		cfg.RuleProviderSubscriptions[idx].LastFailureAt = ""
		shouldApply = hasUsableProxySubscription(cfg)
		return nil
	})
	if err != nil {
		if fetchErr != nil {
			return fmt.Errorf("刷新失败且保存错误状态失败: %w", err)
		}
		return err
	}
	if fetchErr != nil {
		Warnf("规则订阅刷新失败: name=%s url=%s err=%v", name, RedactURL(rp.URL), fetchErr)
		return fetchErr
	}

	// 没有可用代理订阅时，规则订阅只能更新元数据；此时尚不存在可生成的 mihomo 配置。
	if shouldApply {
		if report := d.reconcileLatest("ruleprovider-refresh"); !report.Applied {
			return fmt.Errorf("规则订阅刷新成功，但应用新配置失败: %s", report.Err)
		}
	}
	Infof("规则订阅刷新成功: name=%s url=%s", name, RedactURL(rp.URL))
	return nil
}

func (d *Daemon) handleRuleProviderDetail(w http.ResponseWriter, r *http.Request) {
	name, err := ruleProviderRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	switch r.Method {
	case http.MethodPut:
		if err := d.refreshRuleProvider(name); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("刷新规则订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok("规则订阅已刷新"))
	case http.MethodDelete:
		removed := false
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			if cfg.FindRuleProviderByName(name) < 0 {
				return fmt.Errorf("规则订阅不存在: %s", name)
			}
			removed = true
			return cfg.RemoveRuleProvider(name)
		})
		if err != nil {
			if !removed {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存规则订阅变更失败: %w", err))
			}
			return
		}
		Infof("规则订阅已删除: name=%s", name)
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

func fetchRuleProvider(rawURL string) ([]byte, error) {
	parsed, err := validateSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultIPCRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造规则订阅请求失败: %w", err)
	}
	resp, err := subscriptionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载规则订阅失败: %s", RedactURLInText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("规则订阅服务器返回状态码: %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取规则订阅响应失败: %w", err)
	}
	if int64(len(content)) > maxSubscriptionDownloadSize {
		return nil, fmt.Errorf("规则订阅内容超过 %d MiB 限制", maxSubscriptionDownloadSize>>20)
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return nil, fmt.Errorf("规则订阅内容为空")
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
		return nil, fmt.Errorf("规则订阅返回了 HTML 页面，不是有效规则内容")
	}
	return content, nil
}

func ruleProviderRoute(requestPath string) (string, error) {
	relative := strings.TrimPrefix(requestPath, "/api/v1/rule-providers/")
	if relative == "" || strings.Contains(relative, "/") {
		return "", fmt.Errorf("缺少或无效的规则订阅名称")
	}
	name, err := url.PathUnescape(relative)
	if err != nil {
		return "", fmt.Errorf("规则订阅名称编码无效")
	}
	return name, nil
}

func uniqueRuleProviderName(name string, cfg *Config) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "规则订阅"
	}
	candidate := base
	for i := 2; cfg.FindRuleProviderByName(candidate) >= 0; i++ {
		candidate = fmt.Sprintf("%s%d", base, i)
	}
	return candidate
}

func normalizeRuleProviderBehavior(behavior string) (string, error) {
	behavior = strings.ToLower(strings.TrimSpace(behavior))
	if behavior == "" {
		return "classical", nil
	}
	switch behavior {
	case "classical", "domain", "ipcidr":
		return behavior, nil
	default:
		return "", fmt.Errorf("不支持的规则订阅 behavior: %s", behavior)
	}
}

func normalizeRuleProviderFormat(format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return "yaml", nil
	}
	switch format {
	case "yaml", "text", "mrs":
		return format, nil
	default:
		return "", fmt.Errorf("不支持的规则订阅 format: %s", format)
	}
}
