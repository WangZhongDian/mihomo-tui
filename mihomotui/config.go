package mihomotui

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// 常量已迁移至 constants.go，保留别名避免编译中断
const (
	CONFIG_DIR_NAME    = ConfigDirName
	CONFIG_FILE_NAME   = ConfigFileName
	MIHOMO_CONFIG_NAME = MihomoConfigName
	SUBSCRIPTIONS_DIR  = SubscriptionsDir
)

// SystemConfig 系统设置
type SystemConfig struct {
	AutoStart   bool   `yaml:"auto_start"`
	SystemProxy bool   `yaml:"system_proxy"`
	TUN         bool   `yaml:"tun"`
	Language    string `yaml:"language"`
}

// MihomoConfig mihomo 内核设置
type MihomoConfig struct {
	HTTPPort           int    `yaml:"http_port"`
	SOCKS5Port         int    `yaml:"socks5_port"`
	MixedPort          int    `yaml:"mixed_port"`
	RedirPort          int    `yaml:"redir_port"`
	TProxyPort         int    `yaml:"tproxy_port"`
	AllowLan           bool   `yaml:"allow_lan"`
	IPv6               bool   `yaml:"ipv6"`
	UnifiedDelay       bool   `yaml:"unified_delay"`
	AutoRedirect       bool   `yaml:"auto_redirect"`
	LogLevel           string `yaml:"log_level"`
	TestURL            string `yaml:"test_url"`
	ExternalController string `yaml:"external_controller"`
	Secret             string `yaml:"secret"`
}

// ExternalResources 外部资源下载设置
type ExternalResources struct {
	GeoIP   string `yaml:"geoip"`
	GeoSite string `yaml:"geosite"`
	Mihomo  string `yaml:"mihomo"`
}

// SubscriptionMeta 订阅元数据
// SubscriptionSource 描述订阅内容的来源；缓存始终由 daemon 管理。
type SubscriptionSource string

const (
	SubscriptionSourceURL     SubscriptionSource = "url"
	SubscriptionSourceFile    SubscriptionSource = "file"
	SubscriptionSourceContent SubscriptionSource = "content"
)

// SubscriptionPoolMode controls how enabled members are exposed to mihomo.
type SubscriptionPoolMode string

const (
	// SubscriptionPoolModeFailover exposes only the active member; Members order is primary/backup priority.
	SubscriptionPoolModeFailover SubscriptionPoolMode = "failover"
	// SubscriptionPoolModeMerge exposes every member with a valid cache to mihomo simultaneously.
	SubscriptionPoolModeMerge SubscriptionPoolMode = "merge"
)

// SubscriptionPool 是订阅集合。主备模式下 Members 顺序即故障切换优先级；合并模式下全部成员同时生效。
type SubscriptionPool struct {
	ID               string               `yaml:"id" json:"id"`
	Name             string               `yaml:"name" json:"name"`
	Mode             SubscriptionPoolMode `yaml:"mode,omitempty" json:"mode,omitempty"`
	Members          []string             `yaml:"members" json:"members"`
	ActiveMemberID   string               `yaml:"active_member_id" json:"active_member_id"`
	Enabled          bool                 `yaml:"enabled" json:"enabled"`
	RefreshInterval  int                  `yaml:"refresh_interval" json:"refresh_interval"`
	LastSwitchAt     string               `yaml:"last_switch_at,omitempty" json:"last_switch_at,omitempty"`
	LastSwitchReason string               `yaml:"last_switch_reason,omitempty" json:"last_switch_reason,omitempty"`
	Degraded         bool                 `yaml:"degraded" json:"degraded"`
}

// SubscriptionMeta 保存订阅来源元数据；订阅正文只存在 CacheFile 指向的私有缓存中。
// SubscriptionFetchProxyStrategy controls how remote subscription requests are routed.
type SubscriptionFetchProxyStrategy string

const (
	SubscriptionFetchDirect      SubscriptionFetchProxyStrategy = "direct"
	SubscriptionFetchLocalMihomo SubscriptionFetchProxyStrategy = "local_mihomo"
	SubscriptionFetchSystem      SubscriptionFetchProxyStrategy = "system"
)

type SubscriptionMeta struct {
	ID                    string                         `yaml:"id"`
	Name                  string                         `yaml:"name"`
	URL                   string                         `yaml:"url"`
	UpdatedAt             string                         `yaml:"updated_at"`
	LastSuccessAt         string                         `yaml:"last_success_at"`
	LastFailureAt         string                         `yaml:"last_failure_at,omitempty"`
	LastError             string                         `yaml:"last_error,omitempty"`
	UsedGB                float64                        `yaml:"used_gb"`
	TotalGB               float64                        `yaml:"total_gb"` // 兼容旧配置/旧客户端展示
	UploadBytes           int64                          `yaml:"upload_bytes,omitempty"`
	DownloadBytes         int64                          `yaml:"download_bytes,omitempty"`
	TotalBytes            int64                          `yaml:"total_bytes,omitempty"`
	RemainingBytes        int64                          `yaml:"remaining_bytes,omitempty"`
	ExpireAt              string                         `yaml:"expire_at,omitempty"`
	MetadataAvailable     bool                           `yaml:"metadata_available"`
	MetadataStatus        string                         `yaml:"metadata_status,omitempty"`
	ProfileUpdateInterval int                            `yaml:"profile_update_interval,omitempty"`
	UserAgent             string                         `yaml:"user_agent,omitempty"`
	FetchProxyStrategy    SubscriptionFetchProxyStrategy `yaml:"fetch_proxy_strategy,omitempty"`
	SourceType            SubscriptionSource             `yaml:"source_type,omitempty"`
	CacheFile             string                         `yaml:"cache_file,omitempty"`
	ContentSHA256         string                         `yaml:"content_sha256,omitempty"`
	FailureCount          int                            `yaml:"failure_count,omitempty"`
	LastCheckedAt         string                         `yaml:"last_checked_at,omitempty"`
	UseLocalProxy         bool                           `yaml:"use_local_proxy,omitempty"`
}

// RuleProviderSubscription 规则订阅元数据
type RuleProviderSubscription struct {
	Name          string `yaml:"name"`
	URL           string `yaml:"url"`
	Behavior      string `yaml:"behavior"`    // classical / domain / ipcidr
	Format        string `yaml:"format"`      // yaml / text / mrs，默认 yaml
	Interval      int    `yaml:"interval"`    // 更新间隔（秒），默认 86400
	ProxyGroup    string `yaml:"proxy_group"` // Auto / DIRECT / REJECT，默认 Auto
	UpdatedAt     string `yaml:"updated_at"`
	LastSuccessAt string `yaml:"last_success_at"`
	LastFailureAt string `yaml:"last_failure_at,omitempty"`
	LastError     string `yaml:"last_error,omitempty"`
}

// Config 全局配置
type Config struct {
	// Version 配置版本号，每次成功提交（校验 + 落盘 + 替换内存）递增。
	// 客户端基于读取到的版本提交整份配置，daemon 借此检测陈旧客户端的并发覆盖。
	Version          int64  `yaml:"version" json:"version"`
	MihomoConfigPath string `yaml:"mihomo_config_path"`
	MihomoBinaryPath string `yaml:"mihomo_binary_path"`
	// MihomoActiveVersion 标识 mihomo-tui 私有版本仓库中当前启用的内核。
	MihomoActiveVersion string `yaml:"mihomo_active_version,omitempty"`
	// MihomoRunningVersion 是最近一次经启动确认的实际运行内核版本；
	// API 兼容层只以此字段选择行为，不根据接口失败进行猜测或回退。
	MihomoRunningVersion    string              `yaml:"mihomo_running_version,omitempty"`
	MihomoRunningVersionAt  string              `yaml:"mihomo_running_version_at,omitempty"`
	MihomoVersions          []MihomoVersionInfo `yaml:"mihomo_versions,omitempty"`
	MihomoVersionsCheckedAt string              `yaml:"mihomo_versions_checked_at,omitempty"`
	MihomoVersionsSource    string              `yaml:"mihomo_versions_source,omitempty"`
	MihomoVersionsLastError string              `yaml:"mihomo_versions_last_error,omitempty"`
	System                  SystemConfig        `yaml:"system"`
	Mihomo                  MihomoConfig        `yaml:"mihomo"`
	Subscriptions           []SubscriptionMeta  `yaml:"subscriptions"`
	// SubscriptionPools owns active sources. ActiveSubscription remains for legacy compatibility.
	SubscriptionPools         []SubscriptionPool         `yaml:"subscription_pools,omitempty"`
	ActiveSubscription        int                        `yaml:"active_subscription"`
	RuleProviderSubscriptions []RuleProviderSubscription `yaml:"rule_provider_subscriptions"`
	CustomRules               []string                   `yaml:"custom_rules"`
	ExternalResources         ExternalResources          `yaml:"external_resources"`
	ProxyMode                 string                     `yaml:"proxy_mode"`
	DefaultProxyGroup         string                     `yaml:"default_proxy_group"`
	LogDir                    string                     `yaml:"log_dir"`
	LogLevel                  string                     `yaml:"log_level"`
}

// 下载 URL 常量已迁移至 constants.go，保留别名避免编译中断
const (
	DEFAULT_MIHOMO_DOWNLOAD_URL  = DefaultMihomoDownloadURL
	DEFAULT_GEOIP_DOWNLOAD_URL   = DefaultGeoIPDownloadURL
	DEFAULT_GEOSITE_DOWNLOAD_URL = DefaultGeoSiteDownloadURL
)

var (
	globalConfig    Config
	customConfigDir string
	configMu        sync.RWMutex
)

func init() {
	globalConfig = LoadConfig()
	_ = InitLogger(globalConfig.LogDir, globalConfig.LogLevel)
}

// GlobalConfig 返回全局配置的只读快照（深拷贝指针，线程安全）。
// 调用方可以自由修改返回值，不会影响全局状态；
// 需要修改全局配置时必须使用 UpdateGlobalConfig / ReplaceGlobalConfig，
// 以保证"修改 → 校验 → 落盘 → 替换内存"的原子提交语义。
func GlobalConfig() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	cp := globalConfig.Clone()
	return &cp
}

// SetGlobalConfig 直接替换内存中的全局配置（线程安全），不做校验、不落盘、不递增版本。
// 仅用于客户端缓存同步与测试；daemon 提交配置必须使用 UpdateGlobalConfig / ReplaceGlobalConfig。
func SetGlobalConfig(cfg Config) {
	configMu.Lock()
	defer configMu.Unlock()
	globalConfig = cfg
}

// SetCustomConfigDir 设置自定义配置目录，设置后重新加载配置和日志（线程安全）
func SetCustomConfigDir(dir string) {
	if dir == "" {
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	// 先验证目录可访问，成功后再设置 customConfigDir
	if err := os.MkdirAll(absDir, 0700); err != nil {
		Warnf("无法使用配置目录 %s: %v，继续使用当前目录 %s", absDir, err, GetConfigDir())
		return
	}
	if err := os.Chmod(absDir, 0700); err != nil {
		Warnf("无法收紧配置目录权限 %s: %v", absDir, err)
	}
	configMu.Lock()
	customConfigDir = absDir
	configMu.Unlock()
	// 重新加载配置和日志
	cfg := LoadConfig()
	SetGlobalConfig(cfg)
	if err := InitLogger(cfg.LogDir, cfg.LogLevel); err != nil {
		Warnf("重新初始化日志失败: %v", err)
	}
	Infof("配置目录已设置为: %s", absDir)
}

// GetCustomConfigDir 返回当前自定义配置目录（未设置返回空，线程安全）
func GetCustomConfigDir() string {
	configMu.RLock()
	defer configMu.RUnlock()
	return customConfigDir
}

// defaultConfig 返回默认配置
func defaultConfig() Config {
	return Config{
		MihomoConfigPath: filepath.Join(GetConfigDir(), "mihomo", MIHOMO_CONFIG_NAME),
		System: SystemConfig{
			AutoStart:   false,
			SystemProxy: true,
			TUN:         false,
			Language:    "zh-CN",
		},
		Mihomo: MihomoConfig{
			HTTPPort:           7890,
			SOCKS5Port:         7891,
			MixedPort:          7892,
			RedirPort:          7893,
			TProxyPort:         7894,
			AllowLan:           false,
			IPv6:               true,
			UnifiedDelay:       true,
			AutoRedirect:       false,
			LogLevel:           "info",
			TestURL:            "http://cp.cloudflare.com/generate_204",
			ExternalController: "127.0.0.1:9090",
		},
		Subscriptions:      []SubscriptionMeta{},
		SubscriptionPools:  []SubscriptionPool{},
		ActiveSubscription: -1,
		ExternalResources: ExternalResources{
			GeoIP:   DEFAULT_GEOIP_DOWNLOAD_URL,
			GeoSite: DEFAULT_GEOSITE_DOWNLOAD_URL,
			Mihomo:  DEFAULT_MIHOMO_DOWNLOAD_URL,
		},
		ProxyMode:         "rule",
		DefaultProxyGroup: "Auto",
		LogDir:            filepath.Join(GetConfigDir(), "logs"),
		LogLevel:          "info",
	}
}

// GetConfigDir 返回配置目录路径
// Linux/macOS: ~/.config/mihomo-tui
// Windows: %APPDATA%/mihomo-tui
// 可通过 -d 参数或 SetCustomConfigDir 覆盖
func GetConfigDir() string {
	if customConfigDir != "" {
		return customConfigDir
	}

	var baseDir string

	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = os.Getenv("LOCALAPPDATA")
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, "Library", "Application Support")
	default: // Linux and others
		home, _ := os.UserHomeDir()
		baseDir = os.Getenv("XDG_CONFIG_HOME")
		if baseDir == "" {
			baseDir = filepath.Join(home, ".config")
		}
	}

	if baseDir == "" {
		baseDir = "."
	}

	dir := filepath.Join(baseDir, CONFIG_DIR_NAME)
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "创建配置目录失败: %v\n", err)
		return ""
	}
	if err := os.Chmod(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "设置配置目录权限失败: %v\n", err)
		return ""
	}
	return dir
}

// GetSubscriptionsDir 返回订阅节点文件存储目录
func GetSubscriptionsDir() string {
	dir := filepath.Join(GetConfigDir(), SUBSCRIPTIONS_DIR)
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "创建订阅目录失败: %v\n", err)
		return ""
	}
	if err := os.Chmod(dir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "设置订阅目录权限失败: %v\n", err)
		return ""
	}
	return dir
}

// configFilePath 返回配置文件路径
func configFilePath() string {
	return filepath.Join(GetConfigDir(), CONFIG_FILE_NAME)
}

// LoadConfig 从文件加载配置，文件不存在则返回默认配置
func LoadConfig() Config {
	path := configFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			_ = cfg.Flush()
			return cfg
		}
		fmt.Fprintf(os.Stderr, "读取配置文件失败: %v\n", err)
		return defaultConfig()
	}
	if err := os.Chmod(path, 0600); err != nil {
		Warnf("无法收紧配置文件权限 %s: %v", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "解析配置文件失败: %v\n", err)
		return defaultConfig()
	}

	// 兼容旧配置：为历史订阅补齐稳定 ID，避免后续 API/UI 只能依赖名称定位。
	for i := range cfg.Subscriptions {
		if cfg.Subscriptions[i].ID == "" {
			cfg.Subscriptions[i].ID = newSubscriptionID()
		}
	}
	// 迁移历史配置：所有现有订阅进入默认主备池，保持原列表优先级。
	if len(cfg.Subscriptions) > 0 && len(cfg.SubscriptionPools) == 0 {
		members := make([]string, 0, len(cfg.Subscriptions))
		for i := range cfg.Subscriptions {
			if cfg.Subscriptions[i].SourceType == "" {
				cfg.Subscriptions[i].SourceType = SubscriptionSourceURL
			}
			members = append(members, cfg.Subscriptions[i].ID)
		}
		active := members[0]
		if cfg.ActiveSubscription >= 0 && cfg.ActiveSubscription < len(cfg.Subscriptions) {
			active = cfg.Subscriptions[cfg.ActiveSubscription].ID
		}
		cfg.SubscriptionPools = []SubscriptionPool{{ID: newSubscriptionID(), Name: "默认订阅池", Members: members, ActiveMemberID: active, Enabled: true, RefreshInterval: DayInSeconds}}
		// 延后由 daemon 首次配置提交写回，避免 LoadConfig 期间递归写日志。
	}
	// 自愈：活动订阅索引越界时收敛到合法范围，避免后续提交校验失败。
	if cfg.ActiveSubscription >= len(cfg.Subscriptions) {
		cfg.ActiveSubscription = len(cfg.Subscriptions) - 1
	}
	if cfg.ActiveSubscription < -1 {
		cfg.ActiveSubscription = -1
	}

	// 确保路径字段非空
	if cfg.MihomoConfigPath == "" {
		cfg.MihomoConfigPath = filepath.Join(GetConfigDir(), "mihomo", MIHOMO_CONFIG_NAME)
	}
	if cfg.MihomoBinaryPath == "" && cfg.ExternalResources.Mihomo == "" {
		cfg.ExternalResources.Mihomo = DEFAULT_MIHOMO_DOWNLOAD_URL
	}
	if cfg.System.Language == "" {
		cfg.System.Language = "zh-CN"
	}
	if cfg.Mihomo.LogLevel == "" {
		cfg.Mihomo.LogLevel = "info"
	}
	if cfg.Mihomo.ExternalController == "" {
		cfg.Mihomo.ExternalController = "127.0.0.1:9090"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(GetConfigDir(), "logs")
	}
	if cfg.ProxyMode == "" {
		cfg.ProxyMode = "rule"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.DefaultProxyGroup == "" {
		cfg.DefaultProxyGroup = "Auto"
	}

	Infof("配置加载完成: dir=%s subs=%d active=%d mode=%s default_group=%s", GetConfigDir(), len(cfg.Subscriptions), cfg.ActiveSubscription, cfg.ProxyMode, cfg.DefaultProxyGroup)
	return cfg
}

// Flush 将配置原子写入文件
func (c *Config) Flush() error {
	path := configFilePath()
	// 如果 LogDir 是默认路径，不持久化到 yaml，让不同用户加载时动态计算
	cfg := *c
	if cfg.LogDir == filepath.Join(GetConfigDir(), "logs") {
		cfg.LogDir = ""
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		Errorf("配置序列化失败: %v", err)
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 原子写入：先写临时文件再重命名
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		Errorf("配置写入失败: path=%s err=%v", tmpPath, err)
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		Errorf("配置替换失败: path=%s err=%v", path, err)
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("收紧配置文件权限失败: %w", err)
	}

	Infof("配置已保存: path=%s", path)
	return nil
}

// SanitizeFileName 将订阅名称转为合法文件名
func SanitizeFileName(name string) string {
	// 替换非法字符
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	sanitized := replacer.Replace(name)
	// 限制长度
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return sanitized
}

// FindSubscriptionByName 按显示名称查找订阅索引。
func (c *Config) FindSubscriptionByName(name string) int {
	for i, s := range c.Subscriptions {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// FindSubscriptionByID 按稳定 ID 查找订阅索引。
func (c *Config) FindSubscriptionByID(id string) int {
	for i, s := range c.Subscriptions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// FindSubscriptionByIdentifier 接受稳定 ID；为兼容旧 UI/API，也接受显示名称。
func (c *Config) FindSubscriptionByIdentifier(identifier string) int {
	if idx := c.FindSubscriptionByID(identifier); idx >= 0 {
		return idx
	}
	return c.FindSubscriptionByName(identifier)
}

// AddSubscription 添加订阅（仅修改内存；持久化由 UpdateGlobalConfig 提交）
func (c *Config) AddSubscription(name, url string) error {
	if name == "" {
		return fmt.Errorf("订阅名称不能为空")
	}

	// 检查是否已存在
	idx := c.FindSubscriptionByName(name)
	if idx >= 0 {
		c.Subscriptions[idx].URL = url
	} else {
		c.Subscriptions = append(c.Subscriptions, SubscriptionMeta{
			ID:   newSubscriptionID(),
			Name: name,
			URL:  url,
		})
	}

	return nil
}

// RemoveSubscription 删除订阅（仅修改内存；持久化由 UpdateGlobalConfig 提交）
func (c *Config) RemoveSubscription(name string) error {
	idx := c.FindSubscriptionByName(name)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", name)
	}
	removedID := c.Subscriptions[idx].ID

	// 先同步订阅池成员关系。删除活动成员时选择剩余成员中的首个主备源；
	// 空池保留以便用户后续重新配置，但自动禁用并标为降级状态。
	for pi := range c.SubscriptionPools {
		pool := &c.SubscriptionPools[pi]
		members := pool.Members[:0]
		for _, member := range pool.Members {
			if member != removedID {
				members = append(members, member)
			}
		}
		pool.Members = members
		if pool.ActiveMemberID == removedID {
			if len(pool.Members) > 0 {
				pool.ActiveMemberID = pool.Members[0]
			} else {
				pool.ActiveMemberID = ""
			}
		}
		if len(pool.Members) == 0 {
			pool.Enabled = false
			pool.Degraded = true
			pool.LastSwitchAt = timestampNow()
			pool.LastSwitchReason = "订阅池成员已全部删除"
		}
	}

	c.Subscriptions = append(c.Subscriptions[:idx], c.Subscriptions[idx+1:]...)
	switch {
	case len(c.Subscriptions) == 0:
		c.ActiveSubscription = -1
	case c.ActiveSubscription == idx:
		if idx >= len(c.Subscriptions) {
			c.ActiveSubscription = len(c.Subscriptions) - 1
		} else {
			c.ActiveSubscription = idx
		}
	case c.ActiveSubscription > idx:
		c.ActiveSubscription--
	case c.ActiveSubscription >= len(c.Subscriptions):
		c.ActiveSubscription = len(c.Subscriptions) - 1
	}
	return nil
}

// SetActiveSubscription 设置当前激活订阅（仅修改内存；持久化由 UpdateGlobalConfig 提交）
func (c *Config) SetActiveSubscription(name string) error {
	idx := c.FindSubscriptionByName(name)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", name)
	}
	c.ActiveSubscription = idx
	return nil
}

// SortSubscriptions 按名称排序订阅列表
func (c *Config) SortSubscriptions() {
	sort.Slice(c.Subscriptions, func(i, j int) bool {
		return c.Subscriptions[i].Name < c.Subscriptions[j].Name
	})
}

// ========== 规则订阅辅助方法 ==========

// FindRuleProviderByName 按名称查找规则订阅索引
func (c *Config) FindRuleProviderByName(name string) int {
	for i, rp := range c.RuleProviderSubscriptions {
		if rp.Name == name {
			return i
		}
	}
	return -1
}

// AddRuleProvider 添加规则订阅（仅修改内存；持久化由 UpdateGlobalConfig 提交）
func (c *Config) AddRuleProvider(name, url, behavior, format, proxyGroup string, interval int) error {
	if name == "" {
		return fmt.Errorf("规则订阅名称不能为空")
	}
	if url == "" {
		return fmt.Errorf("规则订阅链接不能为空")
	}
	if behavior == "" {
		behavior = "classical"
	}
	if format == "" {
		format = "yaml"
	}
	if proxyGroup == "" {
		proxyGroup = "Auto"
	}
	if interval <= 0 {
		interval = 86400
	}

	idx := c.FindRuleProviderByName(name)
	if idx >= 0 {
		c.RuleProviderSubscriptions[idx].URL = url
		c.RuleProviderSubscriptions[idx].Behavior = behavior
		c.RuleProviderSubscriptions[idx].Format = format
		c.RuleProviderSubscriptions[idx].ProxyGroup = proxyGroup
		c.RuleProviderSubscriptions[idx].Interval = interval
		c.RuleProviderSubscriptions[idx].UpdatedAt = time.Now().Format("2006-01-02 15:04")
	} else {
		c.RuleProviderSubscriptions = append(c.RuleProviderSubscriptions, RuleProviderSubscription{
			Name:       name,
			URL:        url,
			Behavior:   behavior,
			Format:     format,
			ProxyGroup: proxyGroup,
			Interval:   interval,
			UpdatedAt:  time.Now().Format("2006-01-02 15:04"),
		})
	}

	return nil
}

// RemoveRuleProvider 删除规则订阅（仅修改内存；持久化由 UpdateGlobalConfig 提交）
func (c *Config) RemoveRuleProvider(name string) error {
	idx := c.FindRuleProviderByName(name)
	if idx < 0 {
		return fmt.Errorf("规则订阅不存在: %s", name)
	}

	c.RuleProviderSubscriptions = append(c.RuleProviderSubscriptions[:idx], c.RuleProviderSubscriptions[idx+1:]...)
	return nil
}
