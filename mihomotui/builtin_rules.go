package mihomotui

import (
	"fmt"
	"strings"
)

// BuiltInRuleKind distinguishes literal rules, rule providers and the terminal MATCH rule.
type BuiltInRuleKind string

const (
	BuiltInRuleLiteral  BuiltInRuleKind = "literal"
	BuiltInRuleProvider BuiltInRuleKind = "provider"
	BuiltInRuleMatch    BuiltInRuleKind = "match"
)

// BuiltInRule is a user-manageable built-in rule entry. Provider fields are used only for provider entries.
type BuiltInRule struct {
	ID         string          `yaml:"id" json:"id"`
	Name       string          `yaml:"name" json:"name"`
	Kind       BuiltInRuleKind `yaml:"kind" json:"kind"`
	Enabled    bool            `yaml:"enabled" json:"enabled"`
	Order      int             `yaml:"order" json:"order"`
	Rule       string          `yaml:"rule,omitempty" json:"rule,omitempty"`
	URL        string          `yaml:"url,omitempty" json:"url,omitempty"`
	Behavior   string          `yaml:"behavior,omitempty" json:"behavior,omitempty"`
	Format     string          `yaml:"format,omitempty" json:"format,omitempty"`
	Interval   int             `yaml:"interval,omitempty" json:"interval,omitempty"`
	ProxyGroup string          `yaml:"proxy_group,omitempty" json:"proxy_group,omitempty"`
}

var builtInProviderOrder = []string{"reject", "icloud", "apple", "google", "proxy", "direct", "gfw", "tld-not-cn", "telegramcidr", "cncidr", "lancidr", "applications"}

func defaultBuiltInRules() []BuiltInRule {
	rules := make([]BuiltInRule, 0, len(builtInProviderOrder)+len(DEFAULT_RULES)+2)
	order := 0
	for _, name := range builtInProviderOrder {
		rp := builtInRulesProviders[name]
		format := rp.Format
		if format == "" {
			format = "yaml"
		}
		rules = append(rules, BuiltInRule{ID: "builtin-provider-" + name, Name: name, Kind: BuiltInRuleProvider, Enabled: true, Order: order, URL: rp.URL, Behavior: rp.Behavior, Format: format, Interval: rp.Interval, ProxyGroup: rp.ProxyGroup})
		order++
	}
	rules = append(rules, BuiltInRule{ID: "builtin-ssh", Name: "SSH 直连保护", Kind: BuiltInRuleLiteral, Enabled: true, Order: order, Rule: "DST-PORT,22,DIRECT"})
	order++
	for i, rule := range DEFAULT_RULES {
		rules = append(rules, BuiltInRule{ID: fmt.Sprintf("builtin-default-%02d", i+1), Name: fmt.Sprintf("默认规则 %d", i+1), Kind: BuiltInRuleLiteral, Enabled: true, Order: order, Rule: rule})
		order++
	}
	rules = append(rules, BuiltInRule{ID: "builtin-match", Name: "默认兜底 MATCH", Kind: BuiltInRuleMatch, Enabled: true, Order: order, ProxyGroup: "Auto"})
	return rules
}

func (c *Config) ensureBuiltInRules() {
	if c.BuiltInRulesInitialized {
		return
	}
	c.BuiltInRules = defaultBuiltInRules()
	c.BuiltInRulesInitialized = true
	if len(c.CustomRules) > 0 && len(c.PreCustomRules) == 0 {
		c.PreCustomRules = append([]string(nil), c.CustomRules...)
	}
}

func normalizeRuleGroup(rule, defaultGroup string) string {
	if before, ok := strings.CutSuffix(rule, ",Auto"); ok {
		return before + "," + defaultGroup
	}
	return rule
}

// DefaultBuiltInRules returns a fresh copy of the immutable default rule templates.
func DefaultBuiltInRules() []BuiltInRule { return defaultBuiltInRules() }
