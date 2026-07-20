package mihomotui

import (
	"strings"
	"testing"
)

func TestBuiltInRulesOrderingAndDisable(t *testing.T) {
	c := defaultConfig()
	c.PreCustomRules = []string{"DOMAIN,pre.example,DIRECT"}
	c.PostCustomRules = []string{"DOMAIN,post.example,DIRECT"}
	c.RuleProviderSubscriptions = []RuleProviderSubscription{{Name: "user", URL: "https://example.test/rules", Behavior: "domain", Format: "yaml", Interval: 60, ProxyGroup: "DIRECT"}}
	c.BuiltInRules[0].Enabled = false
	_, rules, err := c.buildRuleConfig()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rules, "\n")
	if strings.Contains(joined, "builtin-provider-reject") {
		t.Fatal("disabled provider was emitted")
	}
	pre, user, post, match := strings.Index(joined, "pre.example"), strings.Index(joined, "custom-user"), strings.Index(joined, "post.example"), strings.LastIndex(joined, "MATCH,")
	if !(pre >= 0 && user > pre && post > user && match > post) {
		t.Fatalf("unexpected order: %s", joined)
	}
}

func TestBuiltInRulesValidationProtectsMatch(t *testing.T) {
	c := defaultConfig()
	c.BuiltInRules[len(c.BuiltInRules)-1].Enabled = false
	if err := c.Validate(); err == nil {
		t.Fatal("disabled MATCH should fail validation")
	}
	c = defaultConfig()
	c.PreCustomRules = []string{"MATCH,DIRECT"}
	if err := c.Validate(); err == nil {
		t.Fatal("custom MATCH should fail validation")
	}
}

func TestLegacyCustomRulesMigration(t *testing.T) {
	c := defaultConfig()
	c.BuiltInRulesInitialized = false
	c.BuiltInRules = nil
	c.CustomRules = []string{"DOMAIN,legacy.example,DIRECT"}
	c.ensureBuiltInRules()
	if len(c.PreCustomRules) != 1 || c.PreCustomRules[0] != c.CustomRules[0] || len(c.BuiltInRules) == 0 {
		t.Fatalf("legacy rules not migrated: %#v", c)
	}
}
