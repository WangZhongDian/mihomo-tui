package mihomotui

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
		return DelayUntested
	}
	latest := history[len(history)-1]
	if latest.Delay == 0 {
		return DelayTimeout
	}
	return latest.Delay
}
