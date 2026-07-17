package mihomotui

import (
	"strings"
	"testing"
)

// TestDefaultConfigPassesValidation 默认配置必须始终通过校验（提交路径的前置条件）。
func TestDefaultConfigPassesValidation(t *testing.T) {
	cfg := defaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaultConfig().Validate() = %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	withSubscription := func(c *Config) {
		c.Subscriptions = []SubscriptionMeta{{ID: "s1", Name: "demo", URL: "https://example.com/sub"}}
		c.ActiveSubscription = 0
	}

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // 为空表示期望校验通过
	}{
		{name: "valid default", mutate: func(c *Config) {}},
		{name: "disabled ports allowed", mutate: func(c *Config) {
			c.Mihomo.HTTPPort = 0
			c.Mihomo.SOCKS5Port = 0
		}},
		{name: "duplicate ports", mutate: func(c *Config) {
			c.Mihomo.HTTPPort = 8080
			c.Mihomo.MixedPort = 8080
		}, wantErr: "端口冲突"},
		{name: "port out of range", mutate: func(c *Config) {
			c.Mihomo.HTTPPort = 70000
		}, wantErr: "超出合法范围"},
		{name: "negative port", mutate: func(c *Config) {
			c.Mihomo.SOCKS5Port = -1
		}, wantErr: "超出合法范围"},
		{name: "bad proxy mode", mutate: func(c *Config) {
			c.ProxyMode = "smart"
		}, wantErr: "代理模式非法"},
		{name: "bad mihomo log level", mutate: func(c *Config) {
			c.Mihomo.LogLevel = "verbose"
		}, wantErr: "mihomo log_level 非法"},
		{name: "bad app log level", mutate: func(c *Config) {
			c.LogLevel = "warning"
		}, wantErr: "应用 log_level 非法"},
		{name: "controller missing port", mutate: func(c *Config) {
			c.Mihomo.ExternalController = "127.0.0.1"
		}, wantErr: "host:port"},
		{name: "controller with scheme", mutate: func(c *Config) {
			c.Mihomo.ExternalController = "http://127.0.0.1:9090"
		}, wantErr: "协议前缀"},
		{name: "controller bad port", mutate: func(c *Config) {
			c.Mihomo.ExternalController = "127.0.0.1:99999"
		}, wantErr: "端口非法"},
		{name: "controller empty allowed", mutate: func(c *Config) {
			c.Mihomo.ExternalController = ""
		}},
		{name: "controller empty host allowed", mutate: func(c *Config) {
			c.Mihomo.ExternalController = ":9090"
		}},
		{name: "bad test url", mutate: func(c *Config) {
			c.Mihomo.TestURL = "ftp://example.com"
		}, wantErr: "test_url"},
		{name: "empty test url allowed", mutate: func(c *Config) {
			c.Mihomo.TestURL = ""
		}},
		{name: "empty default group", mutate: func(c *Config) {
			c.DefaultProxyGroup = ""
		}, wantErr: "默认代理组不能为空"},
		{name: "empty language", mutate: func(c *Config) {
			c.System.Language = ""
		}, wantErr: "界面语言不能为空"},
		{name: "valid subscription", mutate: withSubscription},
		{name: "active index out of range", mutate: func(c *Config) {
			withSubscription(c)
			c.ActiveSubscription = 5
		}, wantErr: "活动订阅索引"},
		{name: "duplicate subscription names", mutate: func(c *Config) {
			c.Subscriptions = []SubscriptionMeta{
				{ID: "s1", Name: "demo", URL: "https://example.com/1"},
				{ID: "s2", Name: "demo", URL: "https://example.com/2"},
			}
		}, wantErr: "订阅名称重复"},
		{name: "subscription missing id", mutate: func(c *Config) {
			c.Subscriptions = []SubscriptionMeta{{Name: "demo", URL: "https://example.com/1"}}
		}, wantErr: "缺少稳定 ID"},
		{name: "subscription empty url", mutate: func(c *Config) {
			c.Subscriptions = []SubscriptionMeta{{ID: "s1", Name: "demo"}}
		}, wantErr: "链接不能为空"},
		{name: "manual subscription allowed", mutate: func(c *Config) {
			c.Subscriptions = []SubscriptionMeta{{ID: "s1", Name: "手动配置", URL: "手动配置"}}
			c.ActiveSubscription = 0
		}},
		{name: "rule provider bad behavior", mutate: func(c *Config) {
			c.RuleProviderSubscriptions = []RuleProviderSubscription{{
				Name: "r", URL: "https://example.com/r", Behavior: "bad", Format: "yaml", Interval: 86400,
			}}
		}, wantErr: "behavior 非法"},
		{name: "rule provider bad format", mutate: func(c *Config) {
			c.RuleProviderSubscriptions = []RuleProviderSubscription{{
				Name: "r", URL: "https://example.com/r", Behavior: "domain", Format: "json", Interval: 86400,
			}}
		}, wantErr: "format 非法"},
		{name: "rule provider bad interval", mutate: func(c *Config) {
			c.RuleProviderSubscriptions = []RuleProviderSubscription{{
				Name: "r", URL: "https://example.com/r", Behavior: "domain", Format: "yaml", Interval: 0,
			}}
		}, wantErr: "更新间隔"},
		{name: "valid rule provider", mutate: func(c *Config) {
			c.RuleProviderSubscriptions = []RuleProviderSubscription{{
				Name: "r", URL: "https://example.com/r", Behavior: "classical", Format: "mrs", Interval: 3600,
			}}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
