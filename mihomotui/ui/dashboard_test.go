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
			ID: "sub-1", Name: "demo",
			URL: "https://token:password@example.com/sub?auth=secret",
		}},
		SubscriptionPools: []mihomotui.SubscriptionPool{{
			ID: "pool-1", Name: "运行池", Enabled: true, Members: []string{"sub-1"}, ActiveMemberID: "sub-1",
		}},
	})

	text := buildSubCardText()
	for _, secret := range []string{"token", "password", "auth=secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("dashboard leaked subscription credential %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "运行池") || !strings.Contains(text, "demo") {
		t.Fatalf("dashboard does not show the running pool and its member: %s", text)
	}
}
