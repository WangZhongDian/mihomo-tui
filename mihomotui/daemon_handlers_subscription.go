package mihomotui

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const maxSubscriptionDownloadSize int64 = 20 << 20 // 20 MiB

var subscriptionHTTPClient = &http.Client{Timeout: DefaultIPCRequestTimeout}

type subscriptionFetchResult struct {
	UsedGB  float64
	TotalGB float64
}

func (d *Daemon) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.Subscriptions))
	case http.MethodPost:
		var req SubscriptionImportRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		if req.Manual {
			if err := d.createManualSubscription(req.Name); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, ok(nil))
			return
		}
		if err := d.importSubscription(req.Name, req.URL); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("导入订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// importSubscription 在写入配置前验证远端订阅，并保存可用于展示的订阅元数据。
func (d *Daemon) importSubscription(requestedName, rawURL string) error {
	result, err := fetchSubscription(rawURL)
	if err != nil {
		return err
	}

	// 原子提交订阅变更：克隆 → 修改 → 校验 → 落盘 → 替换内存。
	// 显式指定的名称按“更新同名订阅”处理；未指定名称时按 URL 去重，
	// 避免重复导入生成不断递增的临时名称，同时保持已有订阅 ID 稳定。
	var name string
	var active bool
	_, err = UpdateGlobalConfig(func(cfg *Config) error {
		name = strings.TrimSpace(requestedName)
		idx := -1
		if name != "" {
			idx = cfg.FindSubscriptionByName(name)
		}
		if idx < 0 {
			idx = findSubscriptionByURL(cfg, rawURL)
		}
		if idx >= 0 {
			name = cfg.Subscriptions[idx].Name
		} else {
			name = uniqueSubscriptionName(name, rawURL, cfg)
		}

		now := time.Now().Format(TimeFormatShort)
		if idx >= 0 {
			cfg.Subscriptions[idx].URL = rawURL
			cfg.Subscriptions[idx].UpdatedAt = now
			cfg.Subscriptions[idx].LastSuccessAt = now
			cfg.Subscriptions[idx].LastError = ""
			cfg.Subscriptions[idx].LastFailureAt = ""
			cfg.Subscriptions[idx].UsedGB = result.UsedGB
			cfg.Subscriptions[idx].TotalGB = result.TotalGB
		} else {
			cfg.Subscriptions = append(cfg.Subscriptions, SubscriptionMeta{
				ID:            newSubscriptionID(),
				Name:          name,
				URL:           rawURL,
				UpdatedAt:     now,
				LastSuccessAt: now,
				UsedGB:        result.UsedGB,
				TotalGB:       result.TotalGB,
			})
			idx = len(cfg.Subscriptions) - 1
		}
		active = cfg.ActiveSubscription == idx
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存订阅失败: %w", err)
	}

	if active {
		if report := d.reconcileLatest("subscription-import"); !report.Applied {
			return fmt.Errorf("订阅导入成功，但应用新配置失败: %s", report.Err)
		}
	}
	Infof("订阅导入成功: name=%s url=%s", name, RedactURL(rawURL))
	return nil
}

func (d *Daemon) createManualSubscription(requestedName string) error {
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = "手动配置"
	}
	_, err := UpdateGlobalConfig(func(cfg *Config) error {
		name = uniqueSubscriptionName(name, "", cfg)
		cfg.Subscriptions = append(cfg.Subscriptions, SubscriptionMeta{
			ID:        newSubscriptionID(),
			Name:      name,
			URL:       "手动配置",
			UpdatedAt: time.Now().Format(TimeFormatShort),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存手动订阅失败: %w", err)
	}
	Infof("已创建手动订阅: name=%s", name)
	return nil
}

func (d *Daemon) refreshSubscription(name string) error {
	cfg := GlobalConfig()
	idx := cfg.FindSubscriptionByIdentifier(name)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", name)
	}
	sub := cfg.Subscriptions[idx]
	resolvedName := sub.Name
	if sub.URL == "" || sub.URL == "手动配置" {
		return fmt.Errorf("手动配置不能从远端刷新")
	}

	// 网络请求在提交前完成；提交时按标识符重新定位订阅，
	// 避免刷新期间订阅被删除导致的索引错乱。
	result, fetchErr := fetchSubscription(sub.URL)
	var active bool
	_, err := UpdateGlobalConfig(func(cfg *Config) error {
		idx := cfg.FindSubscriptionByIdentifier(name)
		if idx < 0 {
			return fmt.Errorf("订阅在刷新期间已被删除: %s", resolvedName)
		}
		now := time.Now().Format(TimeFormatShort)
		if fetchErr != nil {
			// 刷新失败同样提交错误状态，便于 UI 展示失败时间与原因
			cfg.Subscriptions[idx].LastError = fetchErr.Error()
			cfg.Subscriptions[idx].LastFailureAt = now
			return nil
		}
		cfg.Subscriptions[idx].UpdatedAt = now
		cfg.Subscriptions[idx].LastSuccessAt = now
		cfg.Subscriptions[idx].LastError = ""
		cfg.Subscriptions[idx].LastFailureAt = ""
		cfg.Subscriptions[idx].UsedGB = result.UsedGB
		cfg.Subscriptions[idx].TotalGB = result.TotalGB
		active = cfg.ActiveSubscription == idx
		return nil
	})
	if err != nil {
		if fetchErr != nil {
			return fmt.Errorf("刷新失败且保存错误状态失败: %w", err)
		}
		return err
	}
	if fetchErr != nil {
		Warnf("订阅刷新失败: name=%s url=%s err=%v", resolvedName, RedactURL(sub.URL), fetchErr)
		return fetchErr
	}

	if active {
		if report := d.reconcileLatest("subscription-refresh"); !report.Applied {
			return fmt.Errorf("订阅刷新成功，但应用新配置失败: %s", report.Err)
		}
	}
	Infof("订阅刷新成功: name=%s url=%s", resolvedName, RedactURL(sub.URL))
	return nil
}

func (d *Daemon) handleSubscriptionDetail(w http.ResponseWriter, r *http.Request) {
	name, action, err := subscriptionRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	switch r.Method {
	case http.MethodPut:
		if action != "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作: %s", action))
			return
		}
		if err := d.refreshSubscription(name); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("刷新订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok("订阅已刷新"))
	case http.MethodDelete:
		var resolvedName string
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			idx := cfg.FindSubscriptionByIdentifier(name)
			if idx < 0 {
				return fmt.Errorf("订阅不存在: %s", name)
			}
			resolvedName = cfg.Subscriptions[idx].Name
			return cfg.RemoveSubscription(resolvedName)
		})
		if err != nil {
			if resolvedName == "" {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存订阅变更失败: %w", err))
			}
			return
		}
		Infof("订阅已删除: name=%s", resolvedName)
		writeJSON(w, http.StatusOK, ok(nil))
	case http.MethodPost:
		if action != "apply" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作: %s", action))
			return
		}
		var resolvedName string
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			idx := cfg.FindSubscriptionByIdentifier(name)
			if idx < 0 {
				return fmt.Errorf("订阅不存在: %s", name)
			}
			resolvedName = cfg.Subscriptions[idx].Name
			return cfg.SetActiveSubscription(resolvedName)
		})
		if err != nil {
			if resolvedName == "" {
				writeError(w, http.StatusNotFound, err)
			} else {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存订阅变更失败: %w", err))
			}
			return
		}
		// mihomo API 客户端由 reconcile 流程按最新配置重建
		if report := d.reconcileLatest("subscription-apply"); !report.Applied {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("应用订阅失败: %s", report.Err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

func hasUsableProxySubscription(cfg *Config) bool {
	for _, sub := range cfg.Subscriptions {
		if sub.URL != "" && sub.URL != "手动配置" {
			return true
		}
	}
	return false
}

func fetchSubscription(rawURL string) (subscriptionFetchResult, error) {
	parsed, err := validateSubscriptionURL(rawURL)
	if err != nil {
		return subscriptionFetchResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultIPCRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return subscriptionFetchResult{}, fmt.Errorf("构造订阅请求失败: %w", err)
	}
	resp, err := subscriptionHTTPClient.Do(req)
	if err != nil {
		return subscriptionFetchResult{}, fmt.Errorf("下载订阅失败: %s", RedactURLInText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return subscriptionFetchResult{}, fmt.Errorf("订阅服务器返回状态码: %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, maxSubscriptionDownloadSize+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return subscriptionFetchResult{}, fmt.Errorf("读取订阅响应失败: %w", err)
	}
	if int64(len(content)) > maxSubscriptionDownloadSize {
		return subscriptionFetchResult{}, fmt.Errorf("订阅内容超过 %d MiB 限制", maxSubscriptionDownloadSize>>20)
	}
	if err := validateSubscriptionContent(content); err != nil {
		return subscriptionFetchResult{}, err
	}
	return parseSubscriptionUserInfo(resp.Header.Get("subscription-userinfo")), nil
}

// validateSubscriptionContent 在不保存订阅正文的前提下做轻量格式识别，
// 避免把 HTML 错误页或任意垃圾内容标记为“导入成功”。
func validateSubscriptionContent(content []byte) error {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return fmt.Errorf("订阅内容为空")
	}
	if isRecognizedSubscriptionContent(trimmed) {
		return nil
	}
	// 常见订阅正文是 Base64 编码的 Clash YAML 或 URI 列表。不能仅以“可解码”
	// 作为成功条件，否则任意 Base64 垃圾（如 aGVsbG8=）会被误判为有效订阅。
	encoded := strings.ReplaceAll(trimmed, "\n", "")
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(encoded); err == nil && isRecognizedSubscriptionContent(strings.TrimSpace(string(decoded))) {
			return nil
		}
	}
	return fmt.Errorf("无法识别订阅内容格式")
}

func isRecognizedSubscriptionContent(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if strings.Contains(lower, "proxies:") || strings.Contains(lower, "proxy-providers:") {
		return true
	}
	for _, prefix := range []string{"ss://", "ssr://", "vmess://", "vless://", "trojan://", "hysteria://", "hysteria2://", "tuic://", "socks5://"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func validateSubscriptionURL(rawURL string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("订阅链接格式无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("订阅链接仅支持 http 或 https")
	}
	return parsed, nil
}

func parseSubscriptionUserInfo(raw string) subscriptionFetchResult {
	var result subscriptionFetchResult
	for _, item := range strings.Split(raw, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(key) {
		case "upload", "download":
			result.UsedGB += n / (1024 * 1024 * 1024)
		case "total":
			result.TotalGB = n / (1024 * 1024 * 1024)
		}
	}
	return result
}

func findSubscriptionByURL(cfg *Config, rawURL string) int {
	for i, sub := range cfg.Subscriptions {
		if sub.URL == rawURL {
			return i
		}
	}
	return -1
}

func uniqueSubscriptionName(requestedName, rawURL string, cfg *Config) string {
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = deriveSubscriptionName(rawURL)
	}
	base := name
	for i := 2; cfg.FindSubscriptionByIdentifier(name) >= 0; i++ {
		name = fmt.Sprintf("%s%d", base, i)
	}
	return name
}

func deriveSubscriptionName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := strings.TrimSuffix(path.Base(parsed.Path), "/")
		if base != "" && base != "." && base != "/" {
			return base
		}
		if parsed.Hostname() != "" {
			return parsed.Hostname()
		}
	}
	return "订阅"
}

func subscriptionRoute(requestPath string) (name, action string, err error) {
	relative := strings.TrimPrefix(requestPath, "/api/v1/subscriptions/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("缺少订阅名称")
	}
	name, err = url.PathUnescape(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("订阅名称编码无效")
	}
	if len(parts) > 1 {
		action = parts[1]
	}
	return name, action, nil
}
