package mihomotui

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// mihomo 内核日志级别
var validMihomoLogLevels = map[string]bool{
	"debug": true, "info": true, "warning": true, "error": true, "silent": true,
}

// 应用自身日志级别
var validAppLogLevels = map[string]bool{
	"debug": true, "info": true, "warn": true, "error": true,
}

var validProxyModes = map[string]bool{
	"rule": true, "global": true, "direct": true,
}

var validRuleProviderBehaviors = map[string]bool{
	"classical": true, "domain": true, "ipcidr": true,
}

var validRuleProviderFormats = map[string]bool{
	"yaml": true, "text": true, "mrs": true,
}

// Validate 校验配置的所有字段，返回聚合后的全部问题（而非首个错误）。
// 提交路径（UpdateGlobalConfig / ReplaceGlobalConfig）在落盘前强制执行该校验，
// 保证写入磁盘的配置始终合法。
func (c *Config) Validate() error {
	var problems []error
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	// ---- mihomo 端口：0 表示禁用，启用的端口必须在合法范围且互不重复 ----
	ports := []struct {
		name string
		port int
	}{
		{"http_port", c.Mihomo.HTTPPort},
		{"socks5_port", c.Mihomo.SOCKS5Port},
		{"mixed_port", c.Mihomo.MixedPort},
		{"redir_port", c.Mihomo.RedirPort},
		{"tproxy_port", c.Mihomo.TProxyPort},
	}
	seenPorts := map[int]string{}
	for _, p := range ports {
		if p.port == 0 {
			continue
		}
		if p.port < 1 || p.port > 65535 {
			add("mihomo %s %d 超出合法范围 (0 表示禁用, 1-65535)", p.name, p.port)
			continue
		}
		if prev, dup := seenPorts[p.port]; dup {
			add("mihomo 端口冲突: %s 与 %s 都是 %d", p.name, prev, p.port)
		} else {
			seenPorts[p.port] = p.name
		}
	}

	// ---- external-controller：允许留空（禁用 API）；否则必须是 host:port ----
	if ec := strings.TrimSpace(c.Mihomo.ExternalController); ec != "" {
		if strings.Contains(ec, "://") {
			add("mihomo external_controller 不应包含协议前缀: %q", ec)
		} else if _, portStr, err := net.SplitHostPort(ec); err != nil {
			add("mihomo external_controller 格式应为 host:port: %q", ec)
		} else if port, err := strconv.Atoi(portStr); err != nil || port < 1 || port > 65535 {
			add("mihomo external_controller 端口非法: %q", ec)
		}
	}

	// ---- 日志级别 ----
	if !validMihomoLogLevels[c.Mihomo.LogLevel] {
		add("mihomo log_level 非法: %q（可选 debug/info/warning/error/silent）", c.Mihomo.LogLevel)
	}
	if !validAppLogLevels[c.LogLevel] {
		add("应用 log_level 非法: %q（可选 debug/info/warn/error）", c.LogLevel)
	}

	// ---- 延迟测试 URL：允许留空；否则必须是 http/https ----
	if tu := strings.TrimSpace(c.Mihomo.TestURL); tu != "" {
		if u, err := url.Parse(tu); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("mihomo test_url 不是合法的 http/https URL: %q", tu)
		}
	}

	// ---- 外部资源下载地址 ----
	for _, resource := range []struct{ name, rawURL string }{{"GeoIP", c.ExternalResources.GeoIP}, {"GeoSite", c.ExternalResources.GeoSite}} {
		u, err := url.Parse(strings.TrimSpace(resource.rawURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("外部资源 %s 下载地址必须是合法 http/https URL: %q", resource.name, resource.rawURL)
		}
	}

	// ---- 代理模式与默认策略组 ----
	if !validProxyModes[c.ProxyMode] {
		add("代理模式非法: %q（可选 rule/global/direct）", c.ProxyMode)
	}
	if strings.TrimSpace(c.DefaultProxyGroup) == "" {
		add("默认代理组不能为空")
	}
	if strings.TrimSpace(c.System.Language) == "" {
		add("界面语言不能为空")
	}

	// ---- 订阅：名称/ID 非空且唯一，活动索引在合法范围 ----
	subNames := map[string]int{}
	subIDs := map[string]int{}
	for i, s := range c.Subscriptions {
		if strings.TrimSpace(s.Name) == "" {
			add("订阅 #%d 名称不能为空", i+1)
		} else if prev, dup := subNames[s.Name]; dup {
			add("订阅名称重复: %q（第 %d 与第 %d 项）", s.Name, prev+1, i+1)
		} else {
			subNames[s.Name] = i
		}
		if s.ID == "" {
			add("订阅 %q 缺少稳定 ID", s.Name)
		} else if prev, dup := subIDs[s.ID]; dup {
			add("订阅 ID 重复: %q（第 %d 与第 %d 项）", s.ID, prev+1, i+1)
		} else {
			subIDs[s.ID] = i
		}
		if strings.TrimSpace(s.URL) == "" {
			add("订阅 %q 链接不能为空", s.Name)
		}
		if strategy := s.FetchProxyStrategy; strategy != "" && strategy != SubscriptionFetchDirect && strategy != SubscriptionFetchLocalMihomo && strategy != SubscriptionFetchSystem {
			add("订阅 %q 拉取网络策略非法: %q（可选 direct/local_mihomo/system）", s.Name, strategy)
		}
		if len(s.UserAgent) > 512 {
			add("订阅 %q User-Agent 过长", s.Name)
		}
	}
	if c.ActiveSubscription < -1 || c.ActiveSubscription >= len(c.Subscriptions) {
		add("活动订阅索引 %d 越界（订阅数 %d，合法范围 -1 ~ %d）",
			c.ActiveSubscription, len(c.Subscriptions), len(c.Subscriptions)-1)
	}

	// ---- 订阅池：成员唯一归属、顺序和活动成员必须一致 ----
	poolIDs := map[string]bool{}
	memberOwner := map[string]string{}
	for _, pool := range c.SubscriptionPools {
		if strings.TrimSpace(pool.ID) == "" || poolIDs[pool.ID] {
			add("订阅池 ID 为空或重复: %q", pool.ID)
		}
		poolIDs[pool.ID] = true
		if strings.TrimSpace(pool.Name) == "" {
			add("订阅池名称不能为空")
		}
		if mode := normalizedSubscriptionPoolMode(pool.Mode); mode != SubscriptionPoolModeFailover && mode != SubscriptionPoolModeMerge {
			add("订阅池 %q 运行模式非法: %q（可选 failover/merge）", pool.Name, pool.Mode)
		}
		if pool.RefreshInterval < 0 {
			add("订阅池 %q 刷新间隔不能为负数", pool.Name)
		}
		members := map[string]bool{}
		for _, id := range pool.Members {
			if members[id] {
				add("订阅池 %q 成员重复: %s", pool.Name, id)
			}
			members[id] = true
			if c.FindSubscriptionByID(id) < 0 {
				add("订阅池 %q 引用了不存在的订阅: %s", pool.Name, id)
			}
			if old, exists := memberOwner[id]; exists {
				add("订阅 %s 同时属于订阅池 %q 和 %q", id, old, pool.Name)
			} else {
				memberOwner[id] = pool.Name
			}
		}
		if pool.Enabled && len(pool.Members) == 0 {
			add("启用的订阅池 %q 没有成员", pool.Name)
		}
		if pool.ActiveMemberID != "" && !members[pool.ActiveMemberID] {
			add("订阅池 %q 的活动成员不属于该集合", pool.Name)
		}
	}

	// ---- 规则订阅：名称唯一，behavior/format/interval 合法 ----
	rpNames := map[string]int{}
	for i, rp := range c.RuleProviderSubscriptions {
		if strings.TrimSpace(rp.Name) == "" {
			add("规则订阅 #%d 名称不能为空", i+1)
		} else if prev, dup := rpNames[rp.Name]; dup {
			add("规则订阅名称重复: %q（第 %d 与第 %d 项）", rp.Name, prev+1, i+1)
		} else {
			rpNames[rp.Name] = i
		}
		if strings.TrimSpace(rp.URL) == "" {
			add("规则订阅 %q 链接不能为空", rp.Name)
		}
		if !validRuleProviderBehaviors[rp.Behavior] {
			add("规则订阅 %q behavior 非法: %q（可选 classical/domain/ipcidr）", rp.Name, rp.Behavior)
		}
		if !validRuleProviderFormats[rp.Format] {
			add("规则订阅 %q format 非法: %q（可选 yaml/text/mrs）", rp.Name, rp.Format)
		}
		if rp.Interval <= 0 {
			add("规则订阅 %q 更新间隔必须为正数: %d", rp.Name, rp.Interval)
		}
	}

	// ---- 可管理内置规则：MATCH 为固定末尾兜底 ----
	if !c.BuiltInRulesInitialized {
		add("内置规则尚未初始化")
	} else {
		ids := map[string]bool{}
		orders := map[int]bool{}
		matchCount := 0
		for i, rule := range c.BuiltInRules {
			if strings.TrimSpace(rule.ID) == "" || ids[rule.ID] {
				add("内置规则 ID 为空或重复: %q", rule.ID)
			}
			ids[rule.ID] = true
			if rule.Order != i || orders[rule.Order] {
				add("内置规则排序非法: %q", rule.Name)
			}
			orders[rule.Order] = true
			switch rule.Kind {
			case BuiltInRuleLiteral:
				if strings.TrimSpace(rule.Rule) == "" {
					add("内置规则 %q 内容不能为空", rule.Name)
				}
			case BuiltInRuleProvider:
				if strings.TrimSpace(rule.URL) == "" {
					add("内置规则订阅 %q 链接不能为空", rule.Name)
				}
				if !validRuleProviderBehaviors[rule.Behavior] {
					add("内置规则订阅 %q behavior 非法", rule.Name)
				}
				if !validRuleProviderFormats[rule.Format] {
					add("内置规则订阅 %q format 非法", rule.Name)
				}
				if rule.Interval <= 0 {
					add("内置规则订阅 %q 更新间隔必须为正数", rule.Name)
				}
			case BuiltInRuleMatch:
				matchCount++
				if !rule.Enabled {
					add("MATCH 兜底规则不能禁用")
				}
				if i != len(c.BuiltInRules)-1 {
					add("MATCH 兜底规则必须位于最后")
				}
				if strings.TrimSpace(rule.ProxyGroup) == "" {
					add("MATCH 兜底规则策略不能为空")
				}
			default:
				add("内置规则 %q 类型非法: %q", rule.Name, rule.Kind)
			}
		}
		if matchCount != 1 {
			add("必须且只能存在一条 MATCH 兜底规则")
		}
	}
	for _, rule := range append(append([]string{}, c.PreCustomRules...), c.PostCustomRules...) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(rule)), "MATCH,") {
			add("自定义规则不能包含 MATCH: %q", rule)
		}
	}

	return errors.Join(problems...)
}
