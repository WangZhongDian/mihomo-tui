package mihomotui

// APIResponse 通用 IPC API 响应包装
type APIResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ConfigResponse 配置查询响应
type ConfigResponse struct {
	Config Config `json:"config"`
}

// SubscriptionImportRequest 导入订阅请求
type SubscriptionImportRequest struct {
	Name   string `json:"name,omitempty"`
	URL    string `json:"url,omitempty"`
	Manual bool   `json:"manual,omitempty"`
}

// ProxySelectRequest 选择代理请求
type ProxySelectRequest struct {
	Name string `json:"name"`
}

// DelayTestRequest 延迟测试请求
type DelayTestRequest struct {
	URL     string `json:"url,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// MihomoStatusResponse mihomo 进程状态响应
type MihomoStatusResponse struct {
	Running bool `json:"running"`
	PID     int  `json:"pid"`
}

// ProxyDelayResponse 代理延迟测试响应
type ProxyDelayResponse struct {
	Delay int    `json:"delay"`
	Name  string `json:"name,omitempty"`
}

// DaemonInfo 守护进程信息
type DaemonInfo struct {
	LaunchMode string `json:"launch_mode"` // embedded 或 standalone
	IsRoot     bool   `json:"is_root"`
}

// UpgradeProgress mihomo 内核升级进度
type UpgradeProgress struct {
	Status  string `json:"status"`  // idle / downloading / extracting / done / error
	Percent int    `json:"percent"` // 0-100
	Message string `json:"message"` // 状态描述
}

// RuleProviderImportRequest 导入规则订阅请求
type RuleProviderImportRequest struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Behavior   string `json:"behavior"`
	Format     string `json:"format"`
	Interval   int    `json:"interval"`
	ProxyGroup string `json:"proxy_group"`
}
