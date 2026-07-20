package mihomotui

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func generateRandomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 如果系统 RNG 失败，使用时间作为 fallback（极低概率事件）
		Warnf("生成随机 secret 时系统 RNG 失败，使用 fallback: %v", err)
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i * 8))
		}
	}
	return hex.EncodeToString(b)
}

// healthCheckYAML provider 健康检查配置
type healthCheckYAML struct {
	Enable   bool   `yaml:"enable"`
	URL      string `yaml:"url"`
	Interval int    `yaml:"interval"`
}

// providerOverrideYAML provider 覆盖配置
type providerOverrideYAML struct {
	AdditionalPrefix string `yaml:"additional-prefix"`
}

// proxyProviderYAML proxy-provider 配置
type proxyProviderYAML struct {
	URL         string               `yaml:"url,omitempty"`
	Path        string               `yaml:"path,omitempty"`
	Type        string               `yaml:"type"`
	Interval    int                  `yaml:"interval"`
	HealthCheck healthCheckYAML      `yaml:"health-check"`
	Override    providerOverrideYAML `yaml:"override"`
}

// ruleProviderYAML rule-provider 配置
type ruleProviderYAML struct {
	Type       string `yaml:"type"`
	Behavior   string `yaml:"behavior"`
	URL        string `yaml:"url"`
	Path       string `yaml:"path"`
	Format     string `yaml:"format"`
	Interval   int    `yaml:"interval"`
	ProxyGroup string `yaml:"proxy-group,omitempty"`
}

// proxyYAML 固定代理配置（如 DIRECT）
type proxyYAML struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	UDP  bool   `yaml:"udp"`
}

// proxyGroupYAML proxy-group 配置
type proxyGroupYAML struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies,omitempty"`
	Use       []string `yaml:"use,omitempty"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
}

// geoxURLYAML Geo 数据下载地址
type geoxURLYAML struct {
	GeoIP   string `yaml:"geoip"`
	GeoSite string `yaml:"geosite"`
	MMDB    string `yaml:"mmdb"`
	ASN     string `yaml:"asn"`
}

// profileYAML 配置持久化
type profileYAML struct {
	StoreSelected bool `yaml:"store-selected"`
	StoreFakeIP   bool `yaml:"store-fake-ip"`
}

// tunYAML TUN 配置
type tunYAML struct {
	Enable              bool     `yaml:"enable"`
	Stack               string   `yaml:"stack"`
	Device              string   `yaml:"device"`
	DNSHijack           []string `yaml:"dns-hijack"`
	AutoRoute           bool     `yaml:"auto-route"`
	AutoRedirect        bool     `yaml:"auto-redirect"`
	AutoDetectInterface bool     `yaml:"auto-detect-interface"`
	RouteExcludeAddress []string `yaml:"route-exclude-address,omitempty"`
}

// dnsYAML DNS 配置
type dnsYAML struct {
	Enable            bool     `yaml:"enable"`
	IPv6              bool     `yaml:"ipv6"`
	EnhancedMode      string   `yaml:"enhanced-mode"`
	FakeIPFilter      []string `yaml:"fake-ip-filter"`
	DefaultNameserver []string `yaml:"default-nameserver"`
	Nameserver        []string `yaml:"nameserver"`
}

// snifferYAML 流量嗅探配置
type snifferYAML struct {
	Enable     bool           `yaml:"enable"`
	Sniff      map[string]any `yaml:"sniff"`
	SkipDomain []string       `yaml:"skip-domain"`
}

// mihomoConfigYAML 最终生成的 mihomo 配置
type mihomoConfigYAML struct {
	Port               int                          `yaml:"port,omitempty"`
	SocksPort          int                          `yaml:"socks-port,omitempty"`
	MixedPort          int                          `yaml:"mixed-port,omitempty"`
	RedirPort          int                          `yaml:"redir-port,omitempty"`
	TProxyPort         int                          `yaml:"tproxy-port,omitempty"`
	AllowLan           bool                         `yaml:"allow-lan"`
	BindAddress        string                       `yaml:"bind-address"`
	IPv6               bool                         `yaml:"ipv6"`
	UnifiedDelay       bool                         `yaml:"unified-delay"`
	TCPConcurrent      bool                         `yaml:"tcp-concurrent"`
	LogLevel           string                       `yaml:"log-level"`
	Mode               string                       `yaml:"mode"`
	ExternalController string                       `yaml:"external-controller,omitempty"`
	Secret             string                       `yaml:"secret,omitempty"`
	ExternalUI         string                       `yaml:"external-ui"`
	ExternalUIURL      string                       `yaml:"external-ui-url"`
	GeodataMode        bool                         `yaml:"geodata-mode"`
	GeoxURL            geoxURLYAML                  `yaml:"geox-url"`
	FindProcessMode    string                       `yaml:"find-process-mode"`
	Profile            profileYAML                  `yaml:"profile"`
	Sniffer            snifferYAML                  `yaml:"sniffer"`
	Tun                tunYAML                      `yaml:"tun"`
	DNS                dnsYAML                      `yaml:"dns"`
	ProxyProviders     map[string]proxyProviderYAML `yaml:"proxy-providers"`
	Proxies            []proxyYAML                  `yaml:"proxies"`
	ProxyGroups        []proxyGroupYAML             `yaml:"proxy-groups"`
	RuleProviders      map[string]ruleProviderYAML  `yaml:"rule-providers"`
	Rules              []string                     `yaml:"rules"`
}

// 其中Atuo作为代理策略的占位符，后续会根据选择的默认代理策略进行替换
var DEFAULT_RULES = []string{
	"GEOIP,CN,DIRECT",
	"DOMAIN-KEYWORD,-cn,DIRECT",
	"DOMAIN-SUFFIX,cn,DIRECT",
	"DOMAIN-SUFFIX,local,DIRECT",
	"IP-CIDR,127.0.0.0/8,DIRECT",
	"IP-CIDR,192.168.0.0/16,DIRECT",
	"IP-CIDR,172.16.0.0/12,DIRECT",
	"IP-CIDR,10.0.0.0/8,DIRECT",
	"IP-CIDR,224.0.0.0/4,DIRECT",
	"IP-CIDR,100.64.0.0/10,DIRECT",
	"IP-CIDR,fe80::/10,DIRECT",
	"DOMAIN,localhost,DIRECT",
	"DOMAIN, local.adguard.org,DIRECT", // adguard
	"DOMAIN, injections.adguard.org,DIRECT",
	"DOMAIN-SUFFIX,github.io,Auto",
	"DOMAIN-SUFFIX,google.com,Auto",
	"DOMAIN-SUFFIX,facebook.com,Auto",
}

// 其中Atuo作为代理策略的占位符，后续会根据选择的默认代理策略进行替换
var builtInRulesProviders = map[string]ruleProviderYAML{
	"reject": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/reject.txt",
		Path:       "./rules/reject.yaml",
		Interval:   86400,
		ProxyGroup: "REJECT",
	},
	"icloud": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/icloud.txt",
		Path:       "./rules/icloud.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
	"apple": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/apple.txt",
		Path:       "./rules/apple.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
	"google": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/google.txt",
		Path:       "./rules/google.yaml",
		Interval:   86400,
		ProxyGroup: "Auto",
	},
	"proxy": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/proxy.txt",
		Path:       "./rules/proxy.yaml",
		Interval:   86400,
		ProxyGroup: "Auto",
	},
	"direct": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/direct.txt",
		Path:       "./rules/direct.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
	"gfw": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/gfw.txt",
		Path:       "./rules/gfw.yaml",
		Interval:   86400,
		ProxyGroup: "Auto",
	},
	"tld-not-cn": {
		Type:       "http",
		Behavior:   "domain",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/tld-not-cn.txt",
		Path:       "./rules/tld-not-cn.yaml",
		Interval:   86400,
		ProxyGroup: "Auto",
	},
	"telegramcidr": {
		Type:       "http",
		Behavior:   "ipcidr",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/telegramcidr.txt",
		Path:       "./rules/telegramcidr.yaml",
		Interval:   86400,
		ProxyGroup: "Auto",
	},
	"cncidr": {
		Type:       "http",
		Behavior:   "ipcidr",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/cncidr.txt",
		Path:       "./rules/cncidr.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
	"lancidr": {
		Type:       "http",
		Behavior:   "ipcidr",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/lancidr.txt",
		Path:       "./rules/lancidr.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
	"applications": {
		Type:       "http",
		Behavior:   "classical",
		URL:        "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/applications.txt",
		Path:       "./rules/applications.yaml",
		Interval:   86400,
		ProxyGroup: "DIRECT",
	},
}

// ensureSecret 确保 API Secret 已设置（如为空则生成）。
// 仅修改内存；持久化由配置提交层（UpdateGlobalConfig / ReplaceGlobalConfig）负责。
func (c *Config) ensureSecret() error {
	if c.Mihomo.Secret == "" {
		c.Mihomo.Secret = generateRandomSecret()
	}
	return nil
}

// buildProxyConfig 构建代理相关配置（proxy-providers、proxies、proxy-groups）
func (c *Config) buildProxyConfig() (map[string]proxyProviderYAML, []proxyYAML, []proxyGroupYAML, error) {
	// provider 正文由 daemon 主动拉取并写入私有缓存，mihomo 仅读取本地文件。
	activeSubscriptions, err := c.activePoolSubscriptions()
	legacyRemote := false
	if err != nil {
		// 仅供尚未被 daemon 迁移的内存旧配置兼容使用；daemon 正式运行时始终使用本地缓存。
		if len(c.SubscriptionPools) == 0 && len(c.Subscriptions) > 0 {
			activeSubscriptions = c.Subscriptions
			legacyRemote = true
		} else {
			return nil, nil, nil, err
		}
	}

	proxyProviders := make(map[string]proxyProviderYAML)
	providerNames := make([]string, 0, len(activeSubscriptions))
	for i, sub := range activeSubscriptions {
		if legacyRemote && (sub.URL == "" || sub.URL == "手动配置") {
			continue
		}
		providerName := fmt.Sprintf("provider%d", i+1)
		providerNames = append(providerNames, providerName)
		provider := proxyProviderYAML{Type: "file", Interval: 0,
			Path: sub.CacheFile,
			HealthCheck: healthCheckYAML{
				Enable:   true,
				URL:      c.Mihomo.TestURL,
				Interval: HealthCheckInterval,
			},
			Override: providerOverrideYAML{AdditionalPrefix: ""},
		}
		if legacyRemote {
			provider.Type = "http"
			provider.URL = sub.URL
			provider.Path = ""
			provider.Interval = DayInSeconds
		}
		proxyProviders[providerName] = provider
	}
	if len(providerNames) == 0 {
		return nil, nil, nil, fmt.Errorf("没有有效的订阅 URL")
	}

	proxies := []proxyYAML{}

	proxyGroups := []proxyGroupYAML{
		{
			Name:      "Auto",
			Type:      "url-test",
			Use:       providerNames,
			URL:       c.Mihomo.TestURL,
			Interval:  HealthCheckInterval,
			Tolerance: 50,
		},
		{
			Name:    "Manual",
			Type:    "select",
			Use:     providerNames,
			Proxies: []string{"DIRECT"},
		},
		{
			Name:     "Fallback",
			Type:     "fallback",
			Use:      providerNames,
			URL:      c.Mihomo.TestURL,
			Interval: HealthCheckInterval,
		},
		{
			Name:     "Load-Balance",
			Type:     "load-balance",
			Use:      providerNames,
			URL:      c.Mihomo.TestURL,
			Interval: HealthCheckInterval,
		},
	}

	return proxyProviders, proxies, proxyGroups, nil
}

// defaultProxyGroup 返回用户配置的默认代理策略，未设置则返回 Auto
func (c *Config) defaultProxyGroup() string {
	if c.DefaultProxyGroup != "" {
		return c.DefaultProxyGroup
	}
	return "Auto"
}

// buildRuleConfig 构建规则相关配置（rule-providers、rules）。
// 规则顺序由配置持久化，避免 map 遍历导致规则优先级随机变化。
func (c *Config) buildRuleConfig() (map[string]ruleProviderYAML, []string, error) {
	ruleProviders := make(map[string]ruleProviderYAML)
	if c.ProxyMode == "global" {
		return ruleProviders, []string{"MATCH," + c.defaultProxyGroup()}, nil
	}
	if c.ProxyMode == "direct" {
		return ruleProviders, []string{"MATCH,DIRECT"}, nil
	}

	rules := make([]string, 0, len(c.PreCustomRules)+len(c.PostCustomRules)+len(c.BuiltInRules)+len(c.RuleProviderSubscriptions))
	rules = append(rules, c.PreCustomRules...)
	// 用户规则订阅保持配置列表顺序，且始终在内置规则之前。
	for _, rp := range c.RuleProviderSubscriptions {
		providerName, provider, group, ok := c.ruleProviderFromSubscription(rp, "custom-")
		if !ok {
			continue
		}
		ruleProviders[providerName] = provider
		rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", providerName, group))
	}
	for _, entry := range c.BuiltInRules {
		if entry.Kind == BuiltInRuleMatch || !entry.Enabled {
			continue
		}
		switch entry.Kind {
		case BuiltInRuleLiteral:
			rules = append(rules, normalizeRuleGroup(entry.Rule, c.defaultProxyGroup()))
		case BuiltInRuleProvider:
			providerName := "builtin-" + SanitizeFileName(entry.ID)
			format := entry.Format
			if format == "" {
				format = "yaml"
			}
			ext := format
			if ext == "text" {
				ext = "txt"
			}
			group := entry.ProxyGroup
			if group == "" || group == "Auto" {
				group = c.defaultProxyGroup()
			}
			ruleProviders[providerName] = ruleProviderYAML{Type: "http", Behavior: entry.Behavior, URL: entry.URL, Path: fmt.Sprintf("./rules/%s.%s", SanitizeFileName(entry.ID), ext), Format: format, Interval: entry.Interval, ProxyGroup: group}
			rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", providerName, group))
		}
	}
	rules = append(rules, c.PostCustomRules...)
	// MATCH 已由 Validate 约束为唯一且最后；这里固定最后输出以维持兜底语义。
	for _, entry := range c.BuiltInRules {
		if entry.Kind == BuiltInRuleMatch {
			group := entry.ProxyGroup
			if group == "" || group == "Auto" {
				group = c.defaultProxyGroup()
			}
			rules = append(rules, "MATCH,"+group)
			break
		}
	}
	return ruleProviders, rules, nil
}

func (c *Config) ruleProviderFromSubscription(rp RuleProviderSubscription, prefix string) (string, ruleProviderYAML, string, bool) {
	if rp.URL == "" {
		return "", ruleProviderYAML{}, "", false
	}
	format := rp.Format
	if format == "" {
		format = "yaml"
	}
	interval := rp.Interval
	if interval <= 0 {
		interval = DayInSeconds
	}
	group := rp.ProxyGroup
	if group == "" || group == "Auto" {
		group = c.defaultProxyGroup()
	}
	sanitized := SanitizeFileName(rp.Name)
	ext := format
	if ext == "text" {
		ext = "txt"
	}
	hash := md5.Sum([]byte(rp.URL))
	name := prefix + sanitized + "-" + hex.EncodeToString(hash[:4])
	return name, ruleProviderYAML{Type: "http", Behavior: rp.Behavior, URL: rp.URL, Path: fmt.Sprintf("./rules/%s.%s", sanitized, ext), Format: format, Interval: interval, ProxyGroup: group}, group, true
}

// buildGlobalConfig 构建全局基础配置（端口、DNS、TUN、嗅探等）
func (c *Config) buildGlobalConfig() mihomoConfigYAML {
	bindAddress := "*"
	if !c.Mihomo.AllowLan {
		bindAddress = "127.0.0.1"
	}

	return mihomoConfigYAML{
		Port:               c.Mihomo.HTTPPort,
		SocksPort:          c.Mihomo.SOCKS5Port,
		MixedPort:          c.Mihomo.MixedPort,
		RedirPort:          c.Mihomo.RedirPort,
		TProxyPort:         c.Mihomo.TProxyPort,
		AllowLan:           c.Mihomo.AllowLan,
		BindAddress:        bindAddress,
		IPv6:               c.Mihomo.IPv6,
		UnifiedDelay:       c.Mihomo.UnifiedDelay,
		TCPConcurrent:      true,
		LogLevel:           c.Mihomo.LogLevel,
		Mode:               c.ProxyMode,
		ExternalController: c.Mihomo.ExternalController,
		Secret:             c.Mihomo.Secret,
		ExternalUI:         "ui",
		ExternalUIURL:      "https://github.com/MetaCubeX/metacubexd/archive/refs/heads/gh-pages.zip",
		GeodataMode:        true,
		GeoxURL: geoxURLYAML{
			GeoIP:   DefaultGeoIPDownloadURL,
			GeoSite: DefaultGeoSiteDownloadURL,
			MMDB:    "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country-lite.mmdb",
			ASN:     "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/GeoLite2-ASN.mmdb",
		},
		FindProcessMode: "strict",
		Profile: profileYAML{
			StoreSelected: true,
			StoreFakeIP:   true,
		},
		Sniffer: snifferYAML{
			Enable: true,
			Sniff: map[string]any{
				"HTTP": map[string]any{
					"ports":                []any{80, "8080-8880"},
					"override-destination": true,
				},
				"TLS": map[string]any{
					"ports": []any{443, 8443},
				},
				"QUIC": map[string]any{
					"ports": []any{443, 8443},
				},
			},
			SkipDomain: []string{
				"Mijia Cloud",
				"+.push.apple.com",
			},
		},
		Tun: tunYAML{
			Enable:              c.System.TUN,
			Stack:               "mixed",
			Device:              "mihomo-tui-tun",
			DNSHijack:           []string{"any:53", "tcp://any:53"},
			AutoRoute:           true,
			AutoRedirect:        c.Mihomo.AutoRedirect,
			AutoDetectInterface: true,
			RouteExcludeAddress: []string{
				"192.168.0.0/16",
				"10.0.0.0/8",
				"172.16.0.0/12",
				"100.64.0.0/10",
				"127.0.0.0/8",
				"224.0.0.0/4",
				"fe80::/10",
				"fc00::/7",
			},
		},
		DNS: dnsYAML{
			Enable:       true,
			IPv6:         true,
			EnhancedMode: "fake-ip",
			FakeIPFilter: []string{
				"*",
				"+.lan",
				"+.local",
				"+.market.xiaomi.com",
			},
			DefaultNameserver: []string{
				"tls://223.5.5.5",
				"tls://223.6.6.6",
			},
			Nameserver: []string{
				"https://doh.pub/dns-query",
				"https://dns.alidns.com/dns-query",
			},
		},
	}
}

// GenerateMihomoConfig 根据当前激活订阅生成 mihomo 配置文件（使用 proxy-providers 模式）
func (c *Config) GenerateMihomoConfig() error {
	// 1. 确保 Secret
	if err := c.ensureSecret(); err != nil {
		return err
	}

	// 2. 构建代理配置
	proxyProviders, proxies, proxyGroups, err := c.buildProxyConfig()
	if err != nil {
		return err
	}

	// 3. 构建规则配置
	ruleProviders, rules, err := c.buildRuleConfig()
	if err != nil {
		return err
	}

	// 4. 构建全局配置并合并
	mc := c.buildGlobalConfig()
	mc.ProxyProviders = proxyProviders
	mc.Proxies = proxies
	mc.ProxyGroups = proxyGroups
	mc.RuleProviders = ruleProviders
	mc.Rules = rules

	// 5. 序列化并写入文件
	data, err := yaml.Marshal(mc)
	if err != nil {
		return fmt.Errorf("序列化 mihomo 配置失败: %w", err)
	}

	mihomoDir := filepath.Join(GetConfigDir(), "mihomo")
	if err := os.MkdirAll(mihomoDir, 0700); err != nil {
		return fmt.Errorf("创建 mihomo 配置目录失败: %w", err)
	}
	if err := os.Chmod(mihomoDir, 0700); err != nil {
		return fmt.Errorf("收紧 mihomo 配置目录权限失败: %w", err)
	}
	configPath := filepath.Join(mihomoDir, MIHOMO_CONFIG_NAME)

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("写入临时 mihomo 配置失败: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换 mihomo 配置文件失败: %w", err)
	}
	if err := os.Chmod(configPath, 0600); err != nil {
		return fmt.Errorf("收紧 mihomo 配置文件权限失败: %w", err)
	}

	// 同步内存中的 MihomoConfigPath（持久化由配置提交层负责）
	c.MihomoConfigPath = configPath

	return nil
}
