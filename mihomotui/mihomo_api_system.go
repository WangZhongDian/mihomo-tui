package mihomotui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

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

// ========== 解析方法 ==========

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
