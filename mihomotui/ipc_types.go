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
	// RuntimeApply 是 daemon 最近一次运行时应用结果，用于客户端提示
	// "配置已提交但未生效"（例如没有可用的订阅池导致生成失败）。
	RuntimeApply ApplyStatus `json:"runtime_apply"`
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
// SubscriptionUpdateRequest 修改订阅显示名称、远程地址和本地代理拉取策略。
type SubscriptionUpdateRequest struct {
	Name               string                         `json:"name"`
	URL                string                         `json:"url,omitempty"`
	UseLocalProxy      bool                           `json:"use_local_proxy,omitempty"` // 历史兼容：等价于 local_mihomo
	UserAgent          string                         `json:"user_agent,omitempty"`
	FetchProxyStrategy SubscriptionFetchProxyStrategy `json:"fetch_proxy_strategy,omitempty"`
	// Disabled 临时禁用此订阅（订阅池跳过它，但缓存保留）。
	// 指针类型以区分"未传"和"显式传 false"。
	Disabled *bool `json:"disabled,omitempty"`
}

type SubscriptionImportRequest struct {
	Name          string             `json:"name,omitempty"`
	URL           string             `json:"url,omitempty"`
	Manual        bool               `json:"manual,omitempty"`
	SourceType    SubscriptionSource `json:"source_type,omitempty"`
	Content       string             `json:"content,omitempty"` // 文件/粘贴正文；不在响应中回传
	UseLocalProxy bool               `json:"use_local_proxy,omitempty"`
}

// SubscriptionPoolUpdateResponse 订阅池保存响应。
// 配置已持久化即返回成功；Applied/ApplyStage/ApplyError 描述保存后的运行时应用结果，
// 使 UI 能区分"保存失败"与"保存成功但未生效"（例如池被禁用导致无法生成配置）。
type SubscriptionPoolUpdateResponse struct {
	ID         string `json:"id,omitempty"`
	Applied    bool   `json:"applied"`
	ApplyStage string `json:"apply_stage,omitempty"`
	ApplyError string `json:"apply_error,omitempty"`
}

// SubscriptionPoolRequest 创建或更新订阅池；Members 顺序即故障切换优先级。
type SubscriptionPoolRequest struct {
	Name            string               `json:"name"`
	Mode            SubscriptionPoolMode `json:"mode,omitempty"`
	Members         []string             `json:"members"`
	ActiveMemberID  string               `json:"active_member_id,omitempty"`
	Enabled         bool                 `json:"enabled"`
	RefreshInterval int                  `json:"refresh_interval,omitempty"`
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
	Running        bool   `json:"running"`
	PID            int    `json:"pid"`
	RunningVersion string `json:"running_version,omitempty"`
	VersionAt      string `json:"version_at,omitempty"`
	// 最近一次运行时应用结果：配置保存成功但无法生成/重载时，
	// UI 必须能看出内核仍在跑旧配置。
	ConfigApplied     bool   `json:"config_applied"`
	ConfigApplyStage  string `json:"config_apply_stage,omitempty"`
	ConfigApplyError  string `json:"config_apply_error,omitempty"`
	ConfigApplyReason string `json:"config_apply_reason,omitempty"`
	ConfigApplyAt     string `json:"config_apply_at,omitempty"`
}

// ApplyStatus 描述最近一次运行时配置应用结果（daemon 内存态）。
type ApplyStatus struct {
	Applied    bool   `json:"applied"`
	Stage      string `json:"stage,omitempty"`
	Error      string `json:"error,omitempty"`
	Reason     string `json:"reason,omitempty"`
	At         string `json:"at,omitempty"`
	HasApplied bool   `json:"has_applied,omitempty"` // false 表示本次进程尚未执行过应用任务
}

// ProxyDelayResponse 代理延迟测试响应
type ProxyDelayResponse struct {
	Delay int    `json:"delay"`
	Name  string `json:"name,omitempty"`
}

// DaemonInfo 守护进程信息
type DaemonInfo struct {
	LaunchMode         string `json:"launch_mode"` // embedded 或 standalone
	IsRoot             bool   `json:"is_root"`
	CanManageMihomo    bool   `json:"can_manage_mihomo"`    // 当前 IPC 调用方是否可修改内核版本
	CanManageResources bool   `json:"can_manage_resources"` // 当前 IPC 调用方是否可修改外部资源
}

// UpgradeProgress mihomo 内核升级进度
type UpgradeProgress struct {
	Status          string `json:"status"`                     // idle / downloading / extracting / done / error
	Percent         int    `json:"percent"`                    // 0-100
	Message         string `json:"message"`                    // 状态描述
	Version         string `json:"version,omitempty"`          // 当前下载的版本
	DownloadedBytes int64  `json:"downloaded_bytes,omitempty"` // 已下载字节数
	TotalBytes      int64  `json:"total_bytes,omitempty"`      // 总字节数；-1 表示服务端未提供
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

// MihomoVersionsResponse is the cached release catalog plus its refresh metadata.
type MihomoVersionsResponse struct {
	Versions         []MihomoVersionInfo `json:"versions"`
	CheckedAt        string              `json:"checked_at,omitempty"`
	Source           string              `json:"source,omitempty"`
	LastError        string              `json:"last_error,omitempty"`
	ManualImportPath string              `json:"manual_import_path,omitempty"`
}
