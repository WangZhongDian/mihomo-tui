package ui

import (
	"strings"
	"testing"

	"mihomotui/mihomotui"
)

func TestBuildSubCardTextRedactsSubscriptionURL(t *testing.T) {
	old := *mihomotui.GlobalConfig()
	t.Cleanup(func() { mihomotui.SetGlobalConfig(old) })
	mihomotui.SetGlobalConfig(mihomotui.Config{
		Subscriptions: []mihomotui.SubscriptionMeta{{
			Name: "demo",
			URL:  "https://token:password@example.com/sub?auth=secret",
		}},
		ActiveSubscription: 0,
	})

	text := buildSubCardText()
	for _, secret := range []string{"token", "password", "auth=secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("dashboard leaked subscription credential %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "https://example.com/sub") {
		t.Fatalf("dashboard does not show redacted source URL: %s", text)
	}
}
