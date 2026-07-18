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

// ConfigUpdateResponse 配置提交响应。
// 配置本身持久化成功即返回成功；Applied/ApplyStage/ApplyError 描述提交后的
// 运行时应用结果（生成 mihomo 配置、热重载/重启、TUN 同步），
// 使调用方能区分"保存失败"与"保存成功但应用失败"。
type ConfigUpdateResponse struct {
	Config     Config `json:"config"`                // 已提交的配置快照（Secret 已掩码）
	Applied    bool   `json:"applied"`               // 运行时应用是否成功
	ApplyStage string `json:"apply_stage,omitempty"` // 失败阶段
	ApplyError string `json:"apply_error,omitempty"` // 失败原因
}

// SubscriptionImportRequest 导入订阅请求
type SubscriptionImportRequest struct {
	Name          string             `json:"name,omitempty"`
	URL           string             `json:"url,omitempty"`
	Manual        bool               `json:"manual,omitempty"`
	SourceType    SubscriptionSource `json:"source_type,omitempty"`
	Content       string             `json:"content,omitempty"` // 文件/粘贴正文；不在响应中回传
	UseLocalProxy bool               `json:"use_local_proxy,omitempty"`
}

// SubscriptionPoolRequest 创建或更新订阅池；Members 顺序即故障切换优先级。
type SubscriptionPoolRequest struct {
	Name            string   `json:"name"`
	Members         []string `json:"members"`
	ActiveMemberID  string   `json:"active_member_id,omitempty"`
	Enabled         bool     `json:"enabled"`
	RefreshInterval int      `json:"refresh_interval,omitempty"`
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
