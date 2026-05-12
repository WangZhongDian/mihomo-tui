package mihomotui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MihomoAPI mihomo REST API 客户端
type MihomoAPI struct {
	client  *http.Client
	baseURL string
	secret  string
}

// VersionInfo mihomo 版本信息
type VersionInfo struct {
	Meta    bool   `json:"meta"`
	Version string `json:"version"`
}

// ProxyNode 代理节点
type ProxyNode struct {
	Name     string
	Type     string
	Delay    int // ms, -1=超时, -2=测试中, -3=未测试
	Selected bool
}

// ProxyGroup 代理组
type ProxyGroup struct {
	Name  string
	Nodes []ProxyNode
}

// Connection 连接项
type Connection struct {
	ID        string
	Host      string
	Download  float64 // KB
	Upload    float64 // KB
	DownSpeed float64 // B/s
	UpSpeed   float64 // B/s
	Chain     string
	Active    bool
}

// Rule 规则项
type Rule struct {
	Content string
	Type    string
	Policy  string
}

// mihomo API 内部响应结构

type mihomoProxy struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	History []proxyHistory `json:"history"`
}

type proxyHistory struct {
	Time  string `json:"time"`
	Delay int    `json:"delay"`
}

type mihomoProxiesResponse struct {
	Proxies map[string]mihomoProxy `json:"proxies"`
}

type mihomoGroupDelayResponse map[string]int

type mihomoConnectionsResponse struct {
	Connections []struct {
		ID       string `json:"id"`
		Metadata struct {
			Host            string `json:"host"`
			DestinationPort string `json:"destinationPort"`
		} `json:"metadata"`
		Upload   int64    `json:"upload"`
		Download int64    `json:"download"`
		Chains   []string `json:"chains"`
	} `json:"connections"`
}

type mihomoRulesResponse struct {
	Rules []struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
		Proxy   string `json:"proxy"`
	} `json:"rules"`
}

func isGroupType(t string) bool {
	switch t {
	case "Selector", "URLTest", "Fallback", "LoadBalance":
		return true
	}
	return false
}

func getLatestDelay(history []proxyHistory) int {
	if len(history) == 0 {
		return -3 // 未测试
	}
	latest := history[len(history)-1]
	if latest.Delay == 0 {
		return -1 // 超时/失败
	}
	return latest.Delay
}

// NewMihomoAPI 创建 mihomo API 客户端
func NewMihomoAPI(baseURL, secret string) *MihomoAPI {
	return &MihomoAPI{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: baseURL,
		secret:  secret,
	}
}

// NewMihomoAPIFromConfig 从全局配置创建 API 客户端
func NewMihomoAPIFromConfig() *MihomoAPI {
	cfg := GlobalConfig()
	baseURL := "http://" + cfg.Mihomo.ExternalController
	return NewMihomoAPI(baseURL, cfg.Mihomo.Secret)
}

// buildRequest 构造 HTTP 请求
func (c *MihomoAPI) buildRequest(method, path string, body []byte, query map[string]string) (*http.Request, error) {
	url := c.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	return req, nil
}

// do 执行普通 HTTP 请求（带超时）
func (c *MihomoAPI) do(method, path string, body []byte, query map[string]string) (*http.Response, error) {
	req, err := c.buildRequest(method, path, body, query)
	if err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// doStream 执行流式 HTTP 请求（无超时，用于日志/流量/内存/连接等长连接）
func (c *MihomoAPI) doStream(method, path string, query map[string]string) (*http.Response, error) {
	req, err := c.buildRequest(method, path, nil, query)
	if err != nil {
		return nil, err
	}
	// 流式接口不使用超时 client
	client := &http.Client{}
	return client.Do(req)
}

// request 执行请求并读取完整响应 body
func (c *MihomoAPI) request(method, path string, body []byte, query map[string]string) ([]byte, error) {
	resp, err := c.do(method, path, body, query)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// ========== 日志 ==========

// GetLogsStream 获取实时日志流（GET / WS）
// level 可选值: info, warning, error, debug
func (c *MihomoAPI) GetLogsStream(level string) (*http.Response, error) {
	query := make(map[string]string)
	if level != "" {
		query["level"] = level
	}
	return c.doStream(http.MethodGet, "/logs", query)
}

// ========== 流量信息 ==========

// GetTrafficStream 获取实时流量流，单位 kbps（GET / WS）
func (c *MihomoAPI) GetTrafficStream() (*http.Response, error) {
	return c.doStream(http.MethodGet, "/traffic", nil)
}

// ========== 内存信息 ==========

// GetMemoryStream 获取实时内存占用流，单位 kb（GET / WS）
func (c *MihomoAPI) GetMemoryStream() (*http.Response, error) {
	return c.doStream(http.MethodGet, "/memory", nil)
}

// ========== 版本信息 ==========

// GetVersion 获取 mihomo 版本信息
func (c *MihomoAPI) GetVersion() (VersionInfo, error) {
	var info VersionInfo
	data, err := c.request(http.MethodGet, "/version", nil, nil)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}

// ========== 缓存 ==========

// FlushFakeIPCache 清除 fakeip 缓存
func (c *MihomoAPI) FlushFakeIPCache() error {
	_, err := c.request(http.MethodPost, "/cache/fakeip/flush", nil, nil)
	return err
}

// FlushDNSCache 清除 dns 缓存
func (c *MihomoAPI) FlushDNSCache() error {
	_, err := c.request(http.MethodPost, "/cache/dns/flush", nil, nil)
	return err
}

// ========== 运行配置 ==========

// GetConfigs 获取基本配置
func (c *MihomoAPI) GetConfigs() ([]byte, error) {
	return c.request(http.MethodGet, "/configs", nil, nil)
}

// ReloadConfigs 重新加载基本配置（传入配置文件路径）
func (c *MihomoAPI) ReloadConfigs(force bool) error {
	query := make(map[string]string)
	if force {
		query["force"] = "true"
	}
	cfg := GlobalConfig()
	payload, _ := json.Marshal(map[string]string{"path": cfg.MihomoConfigPath})
	_, err := c.request(http.MethodPut, "/configs", payload, query)
	return err
}

// PatchConfigs 更新基本配置
func (c *MihomoAPI) PatchConfigs(payload []byte) error {
	_, err := c.request(http.MethodPatch, "/configs", payload, nil)
	return err
}

// UpdateGeoDB 更新 GEO 数据库
func (c *MihomoAPI) UpdateGeoDB() error {
	_, err := c.request(http.MethodPost, "/configs/geo", []byte(`{"path": "", "payload": ""}`), nil)
	return err
}

// Restart 重启内核
func (c *MihomoAPI) Restart() error {
	_, err := c.request(http.MethodPost, "/restart", []byte(`{"path": "", "payload": ""}`), nil)
	return err
}

// ========== 更新 ==========

// Upgrade 更新内核
func (c *MihomoAPI) Upgrade() error {
	_, err := c.request(http.MethodPost, "/upgrade", []byte(`{"path": "", "payload": ""}`), nil)
	return err
}

// UpgradeUI 更新 UI
func (c *MihomoAPI) UpgradeUI() error {
	_, err := c.request(http.MethodPost, "/upgrade/ui", nil, nil)
	return err
}

// UpgradeGeo 更新 GEO 数据库
func (c *MihomoAPI) UpgradeGeo() error {
	_, err := c.request(http.MethodPost, "/upgrade/geo", []byte(`{"path": "", "payload": ""}`), nil)
	return err
}

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

// ========== 规则 ==========

// GetRules 获取规则信息
func (c *MihomoAPI) GetRules() ([]byte, error) {
	return c.request(http.MethodGet, "/rules", nil, nil)
}

// PatchRulesDisable 禁用规则
func (c *MihomoAPI) PatchRulesDisable(payload []byte) error {
	_, err := c.request(http.MethodPatch, "/rules/disable", payload, nil)
	return err
}

// ========== 规则集合 ==========

// GetRuleProviders 获取所有规则集合的所有信息
func (c *MihomoAPI) GetRuleProviders() ([]byte, error) {
	return c.request(http.MethodGet, "/providers/rules", nil, nil)
}

// UpdateRuleProvider 更新规则集合
func (c *MihomoAPI) UpdateRuleProvider(name string) error {
	_, err := c.request(http.MethodPut, fmt.Sprintf("/providers/rules/%s", name), nil, nil)
	return err
}

// ========== 连接 ==========

// GetConnections 获取连接信息
func (c *MihomoAPI) GetConnections() ([]byte, error) {
	return c.request(http.MethodGet, "/connections", nil, nil)
}

// GetConnectionsStream 获取连接信息流（GET / WS）
func (c *MihomoAPI) GetConnectionsStream(interval int) (*http.Response, error) {
	query := make(map[string]string)
	if interval > 0 {
		query["interval"] = strconv.Itoa(interval)
	}
	return c.doStream(http.MethodGet, "/connections", query)
}

// CloseAllConnections 关闭所有连接
func (c *MihomoAPI) CloseAllConnections() error {
	_, err := c.request(http.MethodDelete, "/connections", nil, nil)
	return err
}

// CloseConnection 关闭特定连接
func (c *MihomoAPI) CloseConnection(id string) error {
	_, err := c.request(http.MethodDelete, fmt.Sprintf("/connections/%s", id), nil, nil)
	return err
}

// ========== 域名查询 ==========

// DNSQuery 获取指定名称和类型的 DNS 查询数据
func (c *MihomoAPI) DNSQuery(name, qtype string) ([]byte, error) {
	query := map[string]string{
		"name": name,
	}
	if qtype != "" {
		query["type"] = qtype
	}
	return c.request(http.MethodGet, "/dns/query", nil, query)
}

// ========== DEBUG ==========

// DebugGC 进行主动 GC
func (c *MihomoAPI) DebugGC() error {
	_, err := c.request(http.MethodPut, "/debug/gc", nil, nil)
	return err
}

var mihomoAPISingleton *MihomoAPI

// GetMihomoAPI 获取 mihomo API 客户端单例
func GetMihomoAPI() (*MihomoAPI, error) {
	if mihomoAPISingleton != nil {
		return mihomoAPISingleton, nil
	}
	cfg := GlobalConfig()
	if cfg.Mihomo.ExternalController == "" {
		return nil, fmt.Errorf("mihomo external-controller 未配置")
	}
	mihomoAPISingleton = NewMihomoAPIFromConfig()
	return mihomoAPISingleton, nil
}

// ResetMihomoAPI 重置 mihomo API 客户端单例
func ResetMihomoAPI() {
	mihomoAPISingleton = nil
}

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
			// 有效延迟（>=0）优先，按值升序（低延迟在前）
			if di >= 0 && dj >= 0 {
				return di < dj
			}
			// 有效延迟始终排在无效状态前面
			if di >= 0 {
				return true
			}
			if dj >= 0 {
				return false
			}
			// 无效状态内部：超时(-1) > 未测试(-3) > 测试中(-2)
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

// GetConnectionsParsed 获取连接列表
func (c *MihomoAPI) GetConnectionsParsed() ([]Connection, error) {
	data, err := c.GetConnections()
	if err != nil {
		return nil, err
	}
	var resp mihomoConnectionsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	conns := make([]Connection, 0, len(resp.Connections))
	for _, conn := range resp.Connections {
		host := conn.Metadata.Host
		if conn.Metadata.DestinationPort != "" {
			host = host + ":" + conn.Metadata.DestinationPort
		}
		conns = append(conns, Connection{
			ID:       conn.ID,
			Host:     host,
			Download: float64(conn.Download) / 1024,
			Upload:   float64(conn.Upload) / 1024,
			Chain:    strings.Join(conn.Chains, " / "),
			Active:   true,
		})
	}
	return conns, nil
}

// GetRulesParsed 获取规则列表
func (c *MihomoAPI) GetRulesParsed() ([]Rule, error) {
	data, err := c.GetRules()
	if err != nil {
		return nil, err
	}
	var resp mihomoRulesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(resp.Rules))
	for _, r := range resp.Rules {
		rules = append(rules, Rule{
			Content: r.Payload,
			Type:    r.Type,
			Policy:  r.Proxy,
		})
	}
	return rules, nil
}
