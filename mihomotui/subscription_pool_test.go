package mihomotui

import (
	"os"
	"testing"
)

func TestSubscriptionPoolMigrationAndCache(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "a", Name: "primary", URL: "https://example.invalid/a"}, {ID: "b", Name: "backup", URL: "https://example.invalid/b"}}
	cfg.ActiveSubscription = 1
	// Mirrors LoadConfig migration intent without relying on a user home config.
	members := []string{"a", "b"}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "默认订阅池", Members: members, ActiveMemberID: "b", Enabled: true, RefreshInterval: defaultSubscriptionRefreshInterval}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.SubscriptionPools[0].ActiveMemberID; got != "b" {
		t.Fatalf("active=%s", got)
	}
	path, digest, err := writeSubscriptionCache("a", []byte("ss://example"))
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Fatal("missing digest")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestFailoverKeepsLastCachedSource(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	p1, _, err := writeSubscriptionCache("primary", []byte("ss://primary"))
	if err != nil {
		t.Fatal(err)
	}
	p2, _, err := writeSubscriptionCache("backup", []byte("ss://backup"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Subscriptions = []SubscriptionMeta{{ID: "primary", Name: "主", URL: "https://bad.invalid", CacheFile: p1, FailureCount: subscriptionFailureThreshold}, {ID: "backup", Name: "备", URL: "https://ok.invalid", CacheFile: p2}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "池", Members: []string{"primary", "backup"}, ActiveMemberID: "primary", Enabled: true}}
	SetGlobalConfig(cfg)
	d := &Daemon{reconcileApply: func(reconcileRequest) ApplyReport { return ApplyReport{Applied: true} }}
	d.failoverSubscription("primary", os.ErrDeadlineExceeded)
	got := GlobalConfig()
	if got.SubscriptionPools[0].ActiveMemberID != "backup" {
		t.Fatalf("failover=%s", got.SubscriptionPools[0].ActiveMemberID)
	}
}

func TestRemoveSubscriptionUpdatesPools(t *testing.T) {
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "a", Name: "one", URL: "https://one.example"}, {ID: "b", Name: "two", URL: "https://two.example"}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "pool", Members: []string{"a", "b"}, ActiveMemberID: "a", Enabled: true}}
	cfg.ActiveSubscription = 0
	if err := cfg.RemoveSubscription("one"); err != nil { t.Fatal(err) }
	pool := cfg.SubscriptionPools[0]
	if len(pool.Members) != 1 || pool.Members[0] != "b" || pool.ActiveMemberID != "b" { t.Fatalf("pool not repaired: %+v", pool) }
	if !pool.Enabled || pool.Degraded { t.Fatalf("pool unexpectedly disabled: %+v", pool) }
	if err := cfg.RemoveSubscription("two"); err != nil { t.Fatal(err) }
	pool = cfg.SubscriptionPools[0]
	if pool.Enabled || !pool.Degraded || pool.ActiveMemberID != "" || len(pool.Members) != 0 { t.Fatalf("empty pool not degraded: %+v", pool) }
}
