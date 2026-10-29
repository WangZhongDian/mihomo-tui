package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestCustomRuleTypeLabels(t *testing.T) {
	labels := customRuleTypeLabels()
	if len(labels) != len(customRuleTypes) {
		t.Fatalf("labels = %d, want %d", len(labels), len(customRuleTypes))
	}
	for i, ruleType := range customRuleTypes {
		if labels[i] != ruleType.kind {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i], ruleType.kind)
		}
	}
}

func TestComposeCustomRule(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		content   string
		policy    string
		noResolve bool
		want      string
	}{
		{name: "domain", kind: "DOMAIN", content: "example.com", policy: "Auto", want: "DOMAIN,example.com,Auto"},
		{name: "trim content", kind: "DOMAIN-SUFFIX", content: "  example.com  ", policy: "DIRECT", want: "DOMAIN-SUFFIX,example.com,DIRECT"},
		{name: "ip cidr no resolve", kind: "IP-CIDR", content: "1.1.1.1/32", policy: "Auto", noResolve: true, want: "IP-CIDR,1.1.1.1/32,Auto,no-resolve"},
		{name: "ipv6 cidr no resolve", kind: "IP-CIDR6", content: "2001:db8::/32", policy: "DIRECT", noResolve: true, want: "IP-CIDR6,2001:db8::/32,DIRECT,no-resolve"},
		{name: "ignore no resolve for domain", kind: "DOMAIN", content: "example.com", policy: "Auto", noResolve: true, want: "DOMAIN,example.com,Auto"},
		{name: "ignore no resolve for source cidr", kind: "SRC-IP-CIDR", content: "192.168.1.0/24", policy: "DIRECT", noResolve: true, want: "SRC-IP-CIDR,192.168.1.0/24,DIRECT"},
		{name: "port", kind: "DST-PORT", content: "443", policy: "DIRECT", want: "DST-PORT,443,DIRECT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := composeCustomRule(tt.kind, tt.content, tt.policy, tt.noResolve)
			if err != nil {
				t.Fatalf("composeCustomRule() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("composeCustomRule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeCustomRuleRejectsInvalidInput(t *testing.T) {
	useTestLanguage(t, "zh-CN")
	tests := []struct {
		name    string
		kind    string
		content string
		wantErr string
	}{
		{name: "empty", kind: "DOMAIN", content: " ", wantErr: "请输入规则内容"},
		{name: "full rule", kind: "DOMAIN", content: "DOMAIN,example.com", wantErr: "只需输入规则内容"},
		{name: "policy included", kind: "DOMAIN", content: "example.com,Auto", wantErr: "只需输入规则内容"},
		{name: "newline", kind: "DOMAIN", content: "example.com\nDIRECT", wantErr: "不能包含换行符"},
		{name: "unknown type", kind: "MATCH", content: "Auto", wantErr: "不支持的规则类型"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := composeCustomRule(tt.kind, tt.content, "Auto", false)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("composeCustomRule() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCustomRulesPageTypeSelection(t *testing.T) {
	page := newCustomRulesPage(tview.NewApplication(), tview.NewPages())
	page.SetRect(0, 0, 110, 20)
	form := page.(*tview.Flex).GetItem(0).(*tview.Flex)
	ruleType := form.GetItem(0).(*tview.DropDown)
	input := form.GetItem(1).(*tview.InputField)
	noResolveRow := form.GetItem(4).(*tview.Flex)
	noResolve := noResolveRow.GetItem(0).(*tview.Checkbox)

	if ruleType.GetOptionCount() != len(customRuleTypes) {
		t.Fatalf("rule type options = %d, want %d", ruleType.GetOptionCount(), len(customRuleTypes))
	}
	if _, kind := ruleType.GetCurrentOption(); kind != "DOMAIN" {
		t.Errorf("default rule type = %q, want DOMAIN", kind)
	}
	if got := renderedText(page, 110, 20); !strings.Contains(got, "example.com") || strings.Contains(got, "no-resolve") {
		t.Errorf("default form does not show only the domain placeholder:\n%s", got)
	}

	ruleType.SetCurrentOption(6) // IP-CIDR
	if got := input.GetText(); got != "" {
		t.Errorf("input changed by selecting a rule type: %q", got)
	}
	if got := renderedText(page, 110, 20); !strings.Contains(got, "no-resolve") || !strings.Contains(got, "1.1.1.1/32") {
		t.Errorf("IP-CIDR form does not show its placeholder and no-resolve:\n%s", got)
	}
	if _, _, width, _ := noResolveRow.GetRect(); width != 16 {
		t.Errorf("IP-CIDR no-resolve width = %d, want 16", width)
	}
	noResolve.SetChecked(true)
	ruleType.SetCurrentOption(0) // DOMAIN
	if noResolve.IsChecked() {
		t.Error("no-resolve remains checked after switching to DOMAIN")
	}
	if got := renderedText(page, 110, 20); strings.Contains(got, "no-resolve") {
		t.Errorf("DOMAIN form still shows no-resolve:\n%s", got)
	}
}

func renderedText(p tview.Primitive, width, height int) string {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		panic(err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	p.Draw(screen)
	screen.Show()
	var text strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			if r == 0 {
				r = ' '
			}
			text.WriteRune(r)
		}
		text.WriteByte('\n')
	}
	return text.String()
}
