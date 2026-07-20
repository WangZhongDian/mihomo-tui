package mihomotui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

// escapeMihomoPathSegment encodes API resource names before embedding them in a path.
// 节点名称常含有空格、#、/ 等字符；未编码的 # 会被当作 URL fragment，
// 而新版 mihomo 对路由匹配更严格，最终表现为测速接口全部失败。
func escapeMihomoPathSegment(name string) string {
	return url.PathEscape(name)
}

const mihomoProviderProxyAPIVersion = "1.19.28"

// usesSeparatedProviderProxyAPI reports whether /proxies omits provider nodes.
// This is a documented REST API break introduced by mihomo v1.19.28.
func usesSeparatedProviderProxyAPI(version string) bool {
	return version != "" && compareMihomoVersions(version, mihomoProviderProxyAPIVersion) >= 0
}

func currentMihomoAPIVersion() string {
	return normalizeMihomoVersion(GlobalConfig().MihomoRunningVersion)
}

// ========== 策略组 ==========

// GetGroups 获取策略组信息
func (c *MihomoAPI) GetGroups() ([]byte, error) {
	return c.request(http.MethodGet, "/group", nil, nil)
}

// GetGroup 获取具体的策略组信息
func (c *MihomoAPI) GetGroup(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/group/%s", escapeMihomoPathSegment(name)), nil, nil)
}

// DeleteGroupFixed 清除自动策略组 fixed 选择
func (c *MihomoAPI) DeleteGroupFixed(name string) error {
	_, err := c.request(http.MethodDelete, fmt.Sprintf("/group/%s", escapeMihomoPathSegment(name)), nil, nil)
	return err
}

// TestGroupDelay 对指定策略组内的节点/策略组进行测试
func (c *MihomoAPI) TestGroupDelay(name, url string, timeout int) ([]byte, error) {
	query := delayTestQuery(url, timeout)
	Infof("开始批量节点测速: group=%q url=%s timeout_ms=%d", name, RedactURLInText(url), timeout)
	data, err := c.request(http.MethodGet, fmt.Sprintf("/group/%s/delay", escapeMihomoPathSegment(name)), nil, query)
	if err != nil {
		Warnf("批量节点测速失败: group=%q url=%s timeout_ms=%d err=%s", name, RedactURLInText(url), timeout, RedactURLInText(err.Error()))
		return nil, err
	}
	Infof("批量节点测速请求完成: group=%q result_bytes=%d", name, len(data))
	return data, nil
}

func delayTestQuery(testURL string, timeout int) map[string]string {
	query := make(map[string]string)
	if testURL != "" {
		query["url"] = testURL
	}
	if timeout > 0 {
		query["timeout"] = strconv.Itoa(timeout)
	}
	return query
}

// ========== 代理 ==========

// GetProxies 获取代理信息
func (c *MihomoAPI) GetProxies() ([]byte, error) {
	return c.request(http.MethodGet, "/proxies", nil, nil)
}

// GetProxy 获取具体的代理信息
func (c *MihomoAPI) GetProxy(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/proxies/%s", escapeMihomoPathSegment(name)), nil, nil)
}

// SelectProxy 选择特定的代理
func (c *MihomoAPI) SelectProxy(name, nodeName string) error {
	Infof("选择代理: %s -> %s", name, nodeName)
	payload, _ := json.Marshal(map[string]string{"name": nodeName})
	response, err := c.request(http.MethodPut, fmt.Sprintf("/proxies/%s", escapeMihomoPathSegment(name)), payload, nil)
	Infof("选择代理结果: %s", string(response))
	return err
}

// TestProxyDelay 对指定代理进行测试
func (c *MihomoAPI) TestProxyDelay(name, url string, timeout int) ([]byte, error) {
	query := delayTestQuery(url, timeout)
	version := currentMihomoAPIVersion()
	Infof("开始节点测速: node=%q url=%s timeout_ms=%d mihomo_version=%q", name, RedactURLInText(url), timeout, version)

	if version == "" {
		err := fmt.Errorf("未记录当前运行的 mihomo 内核版本，请先通过 mihomo-tui 重启内核")
		Warnf("节点测速已拒绝: node=%q err=%s", name, err)
		return nil, err
	}

	if usesSeparatedProviderProxyAPI(version) {
		// v1.19.28+ 恢复原 Clash REST API 语义：provider 节点只存在于
		// /providers/proxies，不能尝试 /proxies/{node}/delay 后再被动回退。
		provider, err := c.findProxyProvider(name)
		if err != nil {
			Warnf("节点测速失败: node=%q mihomo_version=%q provider_lookup_err=%s", name, version, RedactURLInText(err.Error()))
			return nil, fmt.Errorf("mihomo v%s 的节点 %q 无法定位所属订阅 provider: %w", version, name, err)
		}
		Infof("节点测速使用 provider API: node=%q provider=%q mihomo_version=%q", name, provider, version)
		data, err := c.HealthCheckProxyProviderProxy(provider, name, url, timeout)
		if err != nil {
			Warnf("节点测速失败: node=%q provider=%q api=provider-healthcheck mihomo_version=%q url=%s timeout_ms=%d err=%s", name, provider, version, RedactURLInText(url), timeout, RedactURLInText(err.Error()))
			return nil, err
		}
		return data, nil
	}

	// v1.19.27 及更早版本使用 /proxies 接口。
	data, err := c.request(http.MethodGet, fmt.Sprintf("/proxies/%s/delay", escapeMihomoPathSegment(name)), nil, query)
	if err != nil {
		Warnf("节点测速失败: node=%q api=proxies mihomo_version=%q url=%s timeout_ms=%d err=%s", name, version, RedactURLInText(url), timeout, RedactURLInText(err.Error()))
		return nil, err
	}
	return data, nil
}

func (c *MihomoAPI) findProxyProvider(proxyName string) (string, error) {
	data, err := c.GetProxyProviders()
	if err != nil {
		return "", fmt.Errorf("获取 provider 列表失败: %w", err)
	}
	providers, err := parseProxyProviders(data)
	if err != nil {
		return "", err
	}
	for providerName, provider := range providers {
		for _, proxy := range provider.Proxies {
			if proxy.Name == proxyName {
				return providerName, nil
			}
		}
	}
	return "", fmt.Errorf("provider 列表中不存在该节点")
}

func parseProxyProviders(data []byte) (map[string]mihomoProxyProvider, error) {
	var response mihomoProxyProvidersResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("解析 provider 列表失败: %w", err)
	}
	if response.Providers == nil {
		return nil, fmt.Errorf("provider 列表响应缺少 providers 字段")
	}
	return response.Providers, nil
}

// ========== 代理集合 ==========

// GetProxyProviders 获取所有代理集合的所有信息
func (c *MihomoAPI) GetProxyProviders() ([]byte, error) {
	return c.request(http.MethodGet, "/providers/proxies", nil, nil)
}

// GetProxyProvider 获取特定代理集合的信息
func (c *MihomoAPI) GetProxyProvider(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/providers/proxies/%s", escapeMihomoPathSegment(name)), nil, nil)
}

// UpdateProxyProvider 更新代理集合
func (c *MihomoAPI) UpdateProxyProvider(name string) error {
	_, err := c.request(http.MethodPut, fmt.Sprintf("/providers/proxies/%s", escapeMihomoPathSegment(name)), nil, nil)
	return err
}

// HealthCheckProxyProvider 触发特定代理集合的健康检查
func (c *MihomoAPI) HealthCheckProxyProvider(name string) error {
	_, err := c.request(http.MethodGet, fmt.Sprintf("/providers/proxies/%s/healthcheck", escapeMihomoPathSegment(name)), nil, nil)
	return err
}

// HealthCheckProxyProviderProxy 对代理集合内的指定代理进行测试
func (c *MihomoAPI) HealthCheckProxyProviderProxy(provider, proxyName, url string, timeout int) ([]byte, error) {
	query := make(map[string]string)
	if url != "" {
		query["url"] = url
	}
	if timeout > 0 {
		query["timeout"] = strconv.Itoa(timeout)
	}
	path := fmt.Sprintf("/providers/proxies/%s/%s/healthcheck", escapeMihomoPathSegment(provider), escapeMihomoPathSegment(proxyName))
	return c.request(http.MethodGet, path, nil, query)
}

// ========== 解析方法 ==========

// GetProxyGroups 获取代理策略组列表（含节点、延迟、选中状态）
func (c *MihomoAPI) GetProxyGroups() ([]ProxyGroup, error) {
	data, err := c.GetProxies()
	if err != nil {
		return nil, err
	}
	var resp mihomoProxiesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	version := currentMihomoAPIVersion()
	if usesSeparatedProviderProxyAPI(version) {
		// v1.19.28+ 不再将 provider 节点合并进 /proxies；按已记录运行版本
		// 主动从 provider API 补回节点，不依赖请求失败后的被动兼容。
		providerData, providerErr := c.GetProxyProviders()
		if providerErr != nil {
			Warnf("获取 provider 节点列表失败: mihomo_version=%q err=%s", version, RedactURLInText(providerErr.Error()))
		} else if providers, err := parseProxyProviders(providerData); err != nil {
			Warnf("解析 provider 节点列表失败: mihomo_version=%q err=%s", version, err)
		} else {
			providerNodeCount := 0
			for _, provider := range providers {
				for _, proxy := range provider.Proxies {
					if proxy.Name == "" {
						continue
					}
					if _, exists := resp.Proxies[proxy.Name]; !exists {
						resp.Proxies[proxy.Name] = proxy
						providerNodeCount++
					}
				}
			}
			Infof("已从 provider API 合并节点: mihomo_version=%q count=%d", version, providerNodeCount)
		}
	}

	var groups []ProxyGroup
	for name, proxy := range resp.Proxies {
		if !isGroupType(proxy.Type) {
			continue
		}
		group := ProxyGroup{Name: name, Type: proxy.Type, Now: proxy.Now}
		for _, nodeName := range proxy.All {
			nodeProxy, ok := resp.Proxies[nodeName]
			if !ok {
				nodeProxy = mihomoProxy{Name: nodeName, Type: "Direct"}
			}
			delay := getLatestDelay(nodeProxy.History)
			group.Nodes = append(group.Nodes, ProxyNode{
				Name:  nodeName,
				Type:  nodeProxy.Type,
				Delay: delay,
			})
		}
		// 按延迟排序（低延迟优先，超时/未测试/测试中排最后），相同时按名称
		sort.Slice(group.Nodes, func(i, j int) bool {
			di, dj := group.Nodes[i].Delay, group.Nodes[j].Delay
			if di == dj {
				return group.Nodes[i].Name < group.Nodes[j].Name
			}
			if di >= 0 && dj >= 0 {
				return di < dj
			}
			if di >= 0 {
				return true
			}
			if dj >= 0 {
				return false
			}
			return di > dj
		})
		groups = append(groups, group)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups, nil
}

// TestProxyDelayValue 测试指定代理延迟，返回毫秒值（0 表示超时/失败）
func (c *MihomoAPI) TestProxyDelayValue(name, url string, timeout int) (int, error) {
	data, err := c.TestProxyDelay(name, url, timeout)
	if err != nil {
		return 0, err
	}
	var result struct {
		Delay int `json:"delay"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		Warnf("节点测速响应解析失败: node=%q err=%s", name, err)
		return 0, err
	}
	Infof("节点测速完成: node=%q delay_ms=%d", name, result.Delay)
	return result.Delay, nil
}

// TestGroupDelayParsed 测试策略组内节点延迟，返回节点名到延迟的映射
func (c *MihomoAPI) TestGroupDelayParsed(name, url string, timeout int) (map[string]int, error) {
	data, err := c.TestGroupDelay(name, url, timeout)
	if err != nil {
		return nil, err
	}
	var resp mihomoGroupDelayResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		Warnf("批量节点测速响应解析失败: group=%q err=%s", name, err)
		return nil, err
	}
	Infof("批量节点测速完成: group=%q node_count=%d", name, len(resp))
	return resp, nil
}
