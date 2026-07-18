package mihomotui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if err := cfg.RemoveSubscription("one"); err != nil {
		t.Fatal(err)
	}
	pool := cfg.SubscriptionPools[0]
	if len(pool.Members) != 1 || pool.Members[0] != "b" || pool.ActiveMemberID != "b" {
		t.Fatalf("pool not repaired: %+v", pool)
	}
	if !pool.Enabled || pool.Degraded {
		t.Fatalf("pool unexpectedly disabled: %+v", pool)
	}
	if err := cfg.RemoveSubscription("two"); err != nil {
		t.Fatal(err)
	}
	pool = cfg.SubscriptionPools[0]
	if pool.Enabled || !pool.Degraded || pool.ActiveMemberID != "" || len(pool.Members) != 0 {
		t.Fatalf("empty pool not degraded: %+v", pool)
	}
}

func TestUpdateSubscriptionKeepsIdentityAndCache(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "sub-1", Name: "旧名称", URL: "https://old.example/sub", SourceType: SubscriptionSourceURL, CacheFile: "/tmp/kept-cache"}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "pool", Members: []string{"sub-1"}, ActiveMemberID: "sub-1", Enabled: true}}
	SetGlobalConfig(cfg)
	d := &Daemon{}
	body := strings.NewReader(`{"name":"新名称","url":"https://new.example/sub","use_local_proxy":true}`)
	recorder := httptest.NewRecorder()
	d.handleSubscriptionDetail(recorder, httptest.NewRequest(http.MethodPatch, "/api/v1/subscriptions/sub-1", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated := GlobalConfig().Subscriptions[0]
	if updated.ID != "sub-1" || updated.Name != "新名称" || updated.URL != "https://new.example/sub" || !updated.UseLocalProxy || updated.CacheFile != "/tmp/kept-cache" {
		t.Fatalf("unexpected update: %+v", updated)
	}
}

func TestCreatePoolMovesMembersFromDefaultPool(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "sub-1", Name: "one", URL: "https://one.example"}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "default", Name: "默认订阅池", Members: []string{"sub-1"}, ActiveMemberID: "sub-1", Enabled: true}}
	SetGlobalConfig(cfg)
	d := &Daemon{}
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"高可用池","members":["sub-1"],"active_member_id":"sub-1","enabled":true,"refresh_interval":3600}`)
	d.handleSubscriptionPools(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/subscription-pools", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	got := GlobalConfig()
	if len(got.SubscriptionPools) != 2 {
		t.Fatalf("pools=%+v", got.SubscriptionPools)
	}
	if len(got.SubscriptionPools[0].Members) != 0 || got.SubscriptionPools[0].Enabled {
		t.Fatalf("default pool should be empty and disabled: %+v", got.SubscriptionPools[0])
	}
	if members := got.SubscriptionPools[1].Members; len(members) != 1 || members[0] != "sub-1" {
		t.Fatalf("new pool does not own member: %+v", got.SubscriptionPools[1])
	}
}

func TestMergePoolUsesAllCachedMembers(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	one, _, err := writeSubscriptionCache("one", []byte("ss://one"))
	if err != nil {
		t.Fatal(err)
	}
	two, _, err := writeSubscriptionCache("two", []byte("ss://two"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "one", Name: "一", CacheFile: one},
		{ID: "two", Name: "二", CacheFile: two},
		{ID: "missing", Name: "缺失", CacheFile: filepath.Join(t.TempDir(), "missing.yaml")},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{
		ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge, Enabled: true,
		Members: []string{"one", "two", "missing"}, ActiveMemberID: "one",
	}}

	subs, err := cfg.activePoolSubscriptions()
	if err != nil {
		t.Fatalf("activePoolSubscriptions() error = %v", err)
	}
	if len(subs) != 2 || subs[0].ID != "one" || subs[1].ID != "two" {
		t.Fatalf("merged subscriptions = %#v, want both cached members in order", subs)
	}

	providers, _, _, err := cfg.buildProxyConfig()
	if err != nil {
		t.Fatalf("buildProxyConfig() error = %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(providers))
	}
}

func TestMergePoolDoesNotFailOverOnMemberFailure(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	primary, _, err := writeSubscriptionCache("primary", []byte("ss://primary"))
	if err != nil {
		t.Fatal(err)
	}
	backup, _, err := writeSubscriptionCache("backup", []byte("ss://backup"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Subscriptions = []SubscriptionMeta{{ID: "primary", CacheFile: primary, FailureCount: subscriptionFailureThreshold}, {ID: "backup", CacheFile: backup}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge, Members: []string{"primary", "backup"}, ActiveMemberID: "primary", Enabled: true}}
	SetGlobalConfig(cfg)

	(&Daemon{}).failoverSubscription("primary", os.ErrDeadlineExceeded)
	pool := GlobalConfig().SubscriptionPools[0]
	if pool.ActiveMemberID != "primary" || pool.Degraded {
		t.Fatalf("merge pool must not fail over: %+v", pool)
	}
}

func TestSubscriptionPoolModeValidation(t *testing.T) {
	cfg := defaultConfig()
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "bad", Mode: "unexpected", Members: nil, Enabled: false}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "运行模式非法") {
		t.Fatalf("Validate() error = %v, want invalid mode error", err)
	}
}
