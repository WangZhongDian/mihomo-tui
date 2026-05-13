package mihomotui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

// ========== 策略组 ==========

// GetGroups 获取策略组信息
func (c *MihomoAPI) GetGroups() ([]byte, error) {
	return c.request(http.MethodGet, "/group", nil, nil)
}

// GetGroup 获取具体的策略组信息
func (c *MihomoAPI) GetGroup(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/group/%s", name), nil, nil)
}

// DeleteGroupFixed 清除自动策略组 fixed 选择
func (c *MihomoAPI) DeleteGroupFixed(name string) error {
	_, err := c.request(http.MethodDelete, fmt.Sprintf("/group/%s", name), nil, nil)
	return err
}

// TestGroupDelay 对指定策略组内的节点/策略组进行测试
func (c *MihomoAPI) TestGroupDelay(name, url string, timeout int) ([]byte, error) {
	query := make(map[string]string)
	if url != "" {
		query["url"] = url
	}
	if timeout > 0 {
		query["timeout"] = strconv.Itoa(timeout)
	}
	return c.request(http.MethodGet, fmt.Sprintf("/group/%s/delay", name), nil, query)
}

// ========== 代理 ==========

// GetProxies 获取代理信息
func (c *MihomoAPI) GetProxies() ([]byte, error) {
	return c.request(http.MethodGet, "/proxies", nil, nil)
}

// GetProxy 获取具体的代理信息
func (c *MihomoAPI) GetProxy(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/proxies/%s", name), nil, nil)
}

// SelectProxy 选择特定的代理
func (c *MihomoAPI) SelectProxy(name, nodeName string) error {
	Infof("选择代理: %s -> %s", name, nodeName)
	payload, _ := json.Marshal(map[string]string{"name": nodeName})
	response, err := c.request(http.MethodPut, fmt.Sprintf("/proxies/%s", name), payload, nil)
	Infof("选择代理结果: %s", string(response))
	return err
}

// TestProxyDelay 对指定代理进行测试
func (c *MihomoAPI) TestProxyDelay(name, url string, timeout int) ([]byte, error) {
	query := make(map[string]string)
	if url != "" {
		query["url"] = url
	}
	if timeout > 0 {
		query["timeout"] = strconv.Itoa(timeout)
	}
	return c.request(http.MethodGet, fmt.Sprintf("/proxies/%s/delay", name), nil, query)
}

// ========== 代理集合 ==========

// GetProxyProviders 获取所有代理集合的所有信息
func (c *MihomoAPI) GetProxyProviders() ([]byte, error) {
	return c.request(http.MethodGet, "/providers/proxies", nil, nil)
}

// GetProxyProvider 获取特定代理集合的信息
func (c *MihomoAPI) GetProxyProvider(name string) ([]byte, error) {
	return c.request(http.MethodGet, fmt.Sprintf("/providers/proxies/%s", name), nil, nil)
}

// UpdateProxyProvider 更新代理集合
func (c *MihomoAPI) UpdateProxyProvider(name string) error {
	_, err := c.request(http.MethodPut, fmt.Sprintf("/providers/proxies/%s", name), nil, nil)
	return err
}

// HealthCheckProxyProvider 触发特定代理集合的健康检查
func (c *MihomoAPI) HealthCheckProxyProvider(name string) error {
	_, err := c.request(http.MethodGet, fmt.Sprintf("/providers/proxies/%s/healthcheck", name), nil, nil)
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
	path := fmt.Sprintf("/providers/proxies/%s/%s/healthcheck", provider, proxyName)
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

	var groups []ProxyGroup
	for name, proxy := range resp.Proxies {
		if !isGroupType(proxy.Type) {
			continue
		}
		group := ProxyGroup{Name: name}
		for _, nodeName := range proxy.All {
			nodeProxy, ok := resp.Proxies[nodeName]
			if !ok {
				nodeProxy = mihomoProxy{Name: nodeName, Type: "Direct"}
			}
			delay := getLatestDelay(nodeProxy.History)
			group.Nodes = append(group.Nodes, ProxyNode{
				Name:     nodeName,
				Type:     nodeProxy.Type,
				Delay:    delay,
				Selected: nodeName == proxy.Now,
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
		return 0, err
	}
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
		return nil, err
	}
	return resp, nil
}
