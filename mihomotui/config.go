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
type SubscriptionMeta struct {
	Name      string  `yaml:"name"`
	URL       string  `yaml:"url"`
	UpdatedAt string  `yaml:"updated_at"`
	UsedGB    float64 `yaml:"used_gb"`
	TotalGB   float64 `yaml:"total_gb"`
}

// RuleProviderSubscription 规则订阅元数据
type RuleProviderSubscription struct {
	Name       string `yaml:"name"`
	URL        string `yaml:"url"`
	Behavior   string `yaml:"behavior"`    // classical / domain / ipcidr
	Format     string `yaml:"format"`      // yaml / text / mrs，默认 yaml
	Interval   int    `yaml:"interval"`    // 更新间隔（秒），默认 86400
	ProxyGroup string `yaml:"proxy_group"` // Auto / DIRECT / REJECT，默认 Auto
	UpdatedAt  string `yaml:"updated_at"`
}

// Config 全局配置
type Config struct {
	MihomoConfigPath          string                     `yaml:"mihomo_config_path"`
	MihomoBinaryPath          string                     `yaml:"mihomo_binary_path"`
	System                    SystemConfig               `yaml:"system"`
	Mihomo                    MihomoConfig               `yaml:"mihomo"`
	Subscriptions             []SubscriptionMeta         `yaml:"subscriptions"`
	ActiveSubscription        int                        `yaml:"active_subscription"`
	RuleProviderSubscriptions []RuleProviderSubscription `yaml:"rule_provider_subscriptions"`
	CustomRules               []string                   `yaml:"custom_rules"`
	ExternalResources         ExternalResources          `yaml:"external_resources"`
	ProxyMode                 string                     `yaml:"proxy_mode"`
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

// GlobalConfig 返回全局配置单例（线程安全）
func GlobalConfig() *Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return &globalConfig
}

// SetGlobalConfig 更新全局配置单例（线程安全）
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
	if err := os.MkdirAll(absDir, 0755); err != nil {
		Warnf("无法使用配置目录 %s: %v，继续使用当前目录 %s", absDir, err, GetConfigDir())
		return
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
		ActiveSubscription: -1,
		ExternalResources: ExternalResources{
			GeoIP:   DEFAULT_GEOIP_DOWNLOAD_URL,
			GeoSite: DEFAULT_GEOSITE_DOWNLOAD_URL,
			Mihomo:  DEFAULT_MIHOMO_DOWNLOAD_URL,
		},
		ProxyMode: "rule",
		LogDir:    filepath.Join(GetConfigDir(), "logs"),
		LogLevel:  "info",
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "创建配置目录失败: %v\n", err)
		return ""
	}
	return dir
}

// GetSubscriptionsDir 返回订阅节点文件存储目录
func GetSubscriptionsDir() string {
	dir := filepath.Join(GetConfigDir(), SUBSCRIPTIONS_DIR)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "创建订阅目录失败: %v\n", err)
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

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "解析配置文件失败: %v\n", err)
		return defaultConfig()
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

	Infof("配置加载完成: dir=%s subs=%d active=%d mode=%s", GetConfigDir(), len(cfg.Subscriptions), cfg.ActiveSubscription, cfg.ProxyMode)
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
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		Errorf("配置写入失败: path=%s err=%v", tmpPath, err)
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		Errorf("配置替换失败: path=%s err=%v", path, err)
		return fmt.Errorf("替换配置文件失败: %w", err)
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

// FindSubscriptionByName 按名称查找订阅索引
func (c *Config) FindSubscriptionByName(name string) int {
	for i, s := range c.Subscriptions {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// AddSubscription 添加订阅
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
			Name: name,
			URL:  url,
		})
	}

	return c.Flush()
}

// RemoveSubscription 删除订阅
func (c *Config) RemoveSubscription(name string) error {
	idx := c.FindSubscriptionByName(name)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", name)
	}

	// 从列表中移除
	c.Subscriptions = append(c.Subscriptions[:idx], c.Subscriptions[idx+1:]...)

	// 调整激活索引
	if c.ActiveSubscription >= len(c.Subscriptions) {
		c.ActiveSubscription = len(c.Subscriptions) - 1
	}
	if c.ActiveSubscription < 0 && len(c.Subscriptions) > 0 {
		c.ActiveSubscription = 0
	}

	return c.Flush()
}

// SetActiveSubscription 设置当前激活订阅
func (c *Config) SetActiveSubscription(name string) error {
	idx := c.FindSubscriptionByName(name)
	if idx < 0 {
		return fmt.Errorf("订阅不存在: %s", name)
	}
	c.ActiveSubscription = idx
	return c.Flush()
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

// AddRuleProvider 添加规则订阅
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

	return c.Flush()
}

// RemoveRuleProvider 删除规则订阅
func (c *Config) RemoveRuleProvider(name string) error {
	idx := c.FindRuleProviderByName(name)
	if idx < 0 {
		return fmt.Errorf("规则订阅不存在: %s", name)
	}

	c.RuleProviderSubscriptions = append(c.RuleProviderSubscriptions[:idx], c.RuleProviderSubscriptions[idx+1:]...)
	return c.Flush()
}
