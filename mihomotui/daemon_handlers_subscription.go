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
	Content []byte
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
		var err error
		if strings.TrimSpace(req.Content) != "" {
			content := []byte(req.Content)
			err = d.importSubscriptionContent(req.Name, req.URL, normalizedSource(req.SourceType), content, subscriptionFetchResult{Content: content}, req.UseLocalProxy)
		} else {
			result, fetchErr := fetchSubscriptionWithProxy(req.URL, req.UseLocalProxy)
			if fetchErr != nil {
				err = fetchErr
			} else {
				err = d.importSubscriptionContent(req.Name, req.URL, SubscriptionSourceURL, result.Content, result, req.UseLocalProxy)
			}
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("导入订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// importSubscription 下载远端内容并由 daemon 接管为本地缓存。
func (d *Daemon) importSubscription(requestedName, rawURL string) error {
	result, err := fetchSubscriptionWithProxy(rawURL, false)
	if err != nil {
		return err
	}
	return d.importSubscriptionContent(requestedName, rawURL, SubscriptionSourceURL, result.Content, result, false)
}

// importSubscriptionContent 统一处理 URL、文件和粘贴内容；正文绝不进入配置或 IPC 响应。
func (d *Daemon) importSubscriptionContent(requestedName, source string, sourceType SubscriptionSource, content []byte, result subscriptionFetchResult, useLocalProxy bool) error {
	var name string
	var shouldApply bool
	_, err := UpdateGlobalConfig(func(cfg *Config) error {
		name = strings.TrimSpace(requestedName)
		idx := -1
		if name != "" {
			idx = cfg.FindSubscriptionByName(name)
		}
		if idx < 0 && sourceType == SubscriptionSourceURL {
			idx = findSubscriptionByURL(cfg, source)
		}
		if idx >= 0 {
			name = cfg.Subscriptions[idx].Name
		} else {
			name = uniqueSubscriptionName(name, source, cfg)
		}
		var id string
		if idx >= 0 {
			id = cfg.Subscriptions[idx].ID
		} else {
			id = newSubscriptionID()
		}
		cache, digest, cacheErr := writeSubscriptionCache(id, content)
		if cacheErr != nil {
			return cacheErr
		}
		now := timestampNow()
		meta := SubscriptionMeta{ID: id, Name: name, URL: source, SourceType: sourceType, CacheFile: cache, ContentSHA256: digest, UpdatedAt: now, LastSuccessAt: now, LastCheckedAt: now, UsedGB: result.UsedGB, TotalGB: result.TotalGB, UseLocalProxy: useLocalProxy}
		if idx >= 0 {
			cfg.Subscriptions[idx] = meta
		} else {
			cfg.Subscriptions = append(cfg.Subscriptions, meta)
			idx = len(cfg.Subscriptions) - 1
		}
		pool := cfg.ensureDefaultPool()
		present := false
		for _, member := range pool.Members {
			if member == id {
				present = true
				break
			}
		}
		if !present {
			pool.Members = append(pool.Members, id)
		}
		if pool.ActiveMemberID == "" {
			pool.ActiveMemberID = id
		}
		// 保持遗留字段可用。
		cfg.ActiveSubscription = idx
		shouldApply = pool.ActiveMemberID == id && pool.Enabled
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存订阅失败: %w", err)
	}
	if shouldApply {
		if report := d.reconcileLatest("subscription-import"); !report.Applied {
			return fmt.Errorf("订阅已缓存，但应用新配置失败: %s", report.Err)
		}
	}
	Infof("订阅已主动接管: name=%s source=%s", name, RedactURL(source))
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

func (d *Daemon) refreshSubscription(identifier string) error {
	cfg := GlobalConfig()
	idx := cfg.FindSubscriptionByIdentifier(identifier)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", identifier)
	}
	sub := cfg.Subscriptions[idx]
	if normalizedSource(sub.SourceType) != SubscriptionSourceURL || strings.TrimSpace(sub.URL) == "" {
		return fmt.Errorf("订阅 %q 不是可刷新的远程 URL", sub.Name)
	}
	result, fetchErr := fetchSubscriptionWithProxy(sub.URL, sub.UseLocalProxy)
	var active bool
	_, err := UpdateGlobalConfig(func(next *Config) error {
		i := next.FindSubscriptionByID(sub.ID)
		if i < 0 {
			return fmt.Errorf("订阅在刷新期间已被删除: %s", sub.Name)
		}
		now := timestampNow()
		item := &next.Subscriptions[i]
		item.LastCheckedAt = now
		if fetchErr != nil {
			item.FailureCount++
			item.LastError = fetchErr.Error()
			item.LastFailureAt = now
			return nil
		}
		cache, digest, cacheErr := writeSubscriptionCache(item.ID, result.Content)
		if cacheErr != nil {
			return cacheErr
		}
		item.CacheFile = cache
		item.ContentSHA256 = digest
		item.UpdatedAt = now
		item.LastSuccessAt = now
		item.LastError = ""
		item.LastFailureAt = ""
		item.FailureCount = 0
		item.UsedGB = result.UsedGB
		item.TotalGB = result.TotalGB
		for _, pool := range next.SubscriptionPools {
			if pool.ActiveMemberID == item.ID && pool.Enabled {
				active = true
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存刷新状态失败: %w", err)
	}
	if fetchErr != nil {
		d.failoverSubscription(sub.ID, fetchErr)
		return fetchErr
	}
	if active {
		if report := d.reconcileLatest("subscription-refresh"); !report.Applied {
			return fmt.Errorf("订阅已刷新，但应用失败: %s", report.Err)
		}
	}
	return nil
}

// failoverSubscription 在当前活动源连续失败时，按集合顺序选择拥有有效缓存的备用源。
func (d *Daemon) failoverSubscription(failedID string, cause error) {
	var switched bool
	_, err := UpdateGlobalConfig(func(cfg *Config) error {
		for pi := range cfg.SubscriptionPools {
			pool := &cfg.SubscriptionPools[pi]
			// 合并模式没有单一活动源和主备切换；成员独立刷新，失败不能移除
			// 其他缓存节点。
			if normalizedSubscriptionPoolMode(pool.Mode) != SubscriptionPoolModeFailover || !pool.Enabled || pool.ActiveMemberID != failedID {
				continue
			}
			failed := cfg.FindSubscriptionByID(failedID)
			if failed < 0 || cfg.Subscriptions[failed].FailureCount < subscriptionFailureThreshold {
				continue
			}
			for _, candidate := range pool.Members {
				ci := cfg.FindSubscriptionByID(candidate)
				if candidate == failedID || ci < 0 || !hasSubscriptionCache(cfg.Subscriptions[ci]) || cfg.Subscriptions[ci].FailureCount >= subscriptionFailureThreshold {
					continue
				}
				pool.ActiveMemberID = candidate
				pool.Degraded = false
				pool.LastSwitchAt = timestampNow()
				pool.LastSwitchReason = RedactURLInText(cause.Error())
				switched = true
				break
			}
			if !switched {
				pool.Degraded = true
				pool.LastSwitchAt = timestampNow()
				pool.LastSwitchReason = "所有订阅源不可用: " + RedactURLInText(cause.Error())
			}
		}
		return nil
	})
	if err != nil {
		Warnf("更新订阅池故障状态失败: %v", err)
		return
	}
	if switched {
		Infof("订阅池已自动切换备用源: failed=%s", failedID)
		if report := d.reconcileLatest("subscription-failover"); !report.Applied {
			Warnf("订阅池切换后应用失败: %s", report.Err)
		}
	}
}

func (d *Daemon) handleSubscriptionDetail(w http.ResponseWriter, r *http.Request) {
	name, action, err := subscriptionRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		if action != "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作: %s", action))
			return
		}
		var req SubscriptionUpdateRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		var updated SubscriptionMeta
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			idx := cfg.FindSubscriptionByIdentifier(name)
			if idx < 0 {
				return fmt.Errorf("订阅不存在: %s", name)
			}
			item := &cfg.Subscriptions[idx]
			newName := strings.TrimSpace(req.Name)
			if newName == "" {
				return fmt.Errorf("订阅名称不能为空")
			}
			if other := cfg.FindSubscriptionByName(newName); other >= 0 && other != idx {
				return fmt.Errorf("订阅名称已存在: %s", newName)
			}
			if normalizedSource(item.SourceType) == SubscriptionSourceURL {
				if _, err := validateSubscriptionURL(req.URL); err != nil {
					return err
				}
				item.URL = strings.TrimSpace(req.URL)
				item.UseLocalProxy = req.UseLocalProxy
			} else if strings.TrimSpace(req.URL) != "" {
				return fmt.Errorf("本地文件或粘贴订阅不能修改为远程链接，请重新导入")
			}
			item.Name = newName
			item.UpdatedAt = timestampNow()
			updated = *item
			return nil
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("保存订阅修改失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(updated))
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
	return fetchSubscriptionWithProxy(rawURL, false)
}

// fetchSubscriptionWithProxy 可通过当前 mihomo HTTP 代理拉取受限订阅；代理不可用时返回错误，旧缓存不会被覆盖。
func fetchSubscriptionWithProxy(rawURL string, useLocalProxy bool) (subscriptionFetchResult, error) {
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
	client := subscriptionHTTPClient
	if useLocalProxy {
		cfg := GlobalConfig()
		proxyURL, perr := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.Mihomo.HTTPPort))
		if perr != nil {
			return subscriptionFetchResult{}, fmt.Errorf("构造本地代理失败: %w", perr)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyURL(proxyURL)
		client = &http.Client{Timeout: DefaultIPCRequestTimeout, Transport: transport}
	}
	resp, err := client.Do(req)
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
	result := parseSubscriptionUserInfo(resp.Header.Get("subscription-userinfo"))
	result.Content = content
	return result, nil
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
