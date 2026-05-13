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
	URL         string               `yaml:"url"`
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

// ensureSecret 确保 API Secret 已设置（如为空则生成并持久化）
func (c *Config) ensureSecret() error {
	if c.Mihomo.Secret == "" {
		c.Mihomo.Secret = generateRandomSecret()
		if err := c.Flush(); err != nil {
			return fmt.Errorf("保存 secret 失败: %w", err)
		}
	}
	return nil
}

// buildProxyConfig 构建代理相关配置（proxy-providers、proxies、proxy-groups）
func (c *Config) buildProxyConfig() (map[string]proxyProviderYAML, []proxyYAML, []proxyGroupYAML, error) {
	if len(c.Subscriptions) == 0 {
		return nil, nil, nil, fmt.Errorf("没有订阅，请先导入订阅")
	}

	proxyProviders := make(map[string]proxyProviderYAML)
	providerNames := make([]string, 0, len(c.Subscriptions))
	for i, sub := range c.Subscriptions {
		if sub.URL == "" || sub.URL == "手动配置" {
			continue
		}
		providerName := fmt.Sprintf("provider%d", i+1)
		providerNames = append(providerNames, providerName)
		proxyProviders[providerName] = proxyProviderYAML{
			URL:      sub.URL,
			Type:     "http",
			Interval: DayInSeconds,
			HealthCheck: healthCheckYAML{
				Enable:   true,
				URL:      "https://www.gstatic.com/generate_204",
				Interval: HealthCheckInterval,
			},
			Override: providerOverrideYAML{
				AdditionalPrefix: "",
			},
		}
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
			URL:       "http://www.gstatic.com/generate_204",
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
			Name: "Fallback",
			Type: "fallback",
			Use:  providerNames,
			URL:  "http://www.gstatic.com/generate_204",
		},
	}

	return proxyProviders, proxies, proxyGroups, nil
}

// buildRuleConfig 构建规则相关配置（rule-providers、rules）
func (c *Config) buildRuleConfig() (map[string]ruleProviderYAML, []string, error) {
	ruleProviders := make(map[string]ruleProviderYAML)
	rpProxyGroups := make(map[string]string)

	for _, rp := range c.RuleProviderSubscriptions {
		if rp.URL == "" {
			continue
		}
		format := rp.Format
		if format == "" {
			format = "yaml"
		}
		interval := rp.Interval
		if interval <= 0 {
			interval = DayInSeconds
		}
		proxyGroup := rp.ProxyGroup
		if proxyGroup == "" {
			proxyGroup = "Auto"
		}
		sanitized := SanitizeFileName(rp.Name)
		ext := format
		if ext == "text" {
			ext = "txt"
		}
		prefix := sanitized
		if len(prefix) > 3 {
			prefix = prefix[:3]
		}
		hash := md5.Sum([]byte(rp.URL))
		providerName := prefix + hex.EncodeToString(hash[:])
		ruleProviders[providerName] = ruleProviderYAML{
			Type:       "http",
			Behavior:   rp.Behavior,
			URL:        rp.URL,
			Path:       fmt.Sprintf("./rules/%s.%s", sanitized, ext),
			Format:     format,
			Interval:   interval,
			ProxyGroup: proxyGroup,
		}
		rpProxyGroups[providerName] = proxyGroup
	}

	rules := make([]string, 0)

	switch c.ProxyMode {
	case "global":
		rules = append(rules, "MATCH,Auto")
	case "direct":
		rules = append(rules, "MATCH,DIRECT")
	case "rule":
		// SSH 流量必须直连，防止 TUN 劫持后误走代理导致远程服务器 SSH 卡顿/断开
		rules = append(rules, "DST-PORT,22,DIRECT")
		for name := range ruleProviders {
			pg := rpProxyGroups[name]
			if pg == "" {
				pg = "Auto"
			}
			rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", name, pg))
		}

		// 加载内置规则提供者
		for name, rp := range builtInRulesProviders {
			if _, exists := ruleProviders[name]; !exists {
				ruleProviders[name] = rp
				rules = append(rules, fmt.Sprintf("RULE-SET,%s,%s", name, rp.ProxyGroup))
			}
		}
		rules = append(rules, DEFAULT_RULES...)
		rules = append(rules, "MATCH,Auto")
	}
	return ruleProviders, rules, nil
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
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		return fmt.Errorf("创建 mihomo 配置目录失败: %w", err)
	}
	configPath := filepath.Join(mihomoDir, MIHOMO_CONFIG_NAME)

	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("写入临时 mihomo 配置失败: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换 mihomo 配置文件失败: %w", err)
	}

	// 同步更新配置中的 MihomoConfigPath
	c.MihomoConfigPath = configPath
	if err := c.Flush(); err != nil {
		return fmt.Errorf("保存配置路径更新失败: %w", err)
	}

	return nil
}
