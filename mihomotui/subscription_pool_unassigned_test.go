package mihomotui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestCache 写入一份可用的订阅缓存并返回其路径。
func writeTestCache(t *testing.T, id string) string {
	t.Helper()
	path, _, err := writeSubscriptionCache(id, []byte("proxies:\n  - {name: "+id+"-节点, type: ss, server: 1.2.3.4, port: 1, cipher: aes-128-gcm, password: x}\n"))
	if err != nil {
		t.Fatalf("writeSubscriptionCache(%s): %v", id, err)
	}
	return path
}

// 回归：新导入的订阅不自动进入任何订阅池，未分配即不参与运行。
// 旧实现会把订阅塞进 SubscriptionPools[0]（可能已被禁用甚至搬空），
// 造成"导入成功但内核只有第一个订阅的节点"。
func TestImportDoesNotJoinAnyPool(t *testing.T) {
	useTestConfigDir(t)
	SetGlobalConfig(defaultConfig())

	d := &Daemon{}
	body := `{"name":"新订阅","url":"粘贴内容","source_type":"content","content":"proxies:\n  - {name: n1, type: ss, server: 1.2.3.4, port: 1, cipher: aes-128-gcm, password: x}\n"}`
	w := httptest.NewRecorder()
	d.handleSubscriptions(w, httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", w.Code, w.Body.String())
	}

	cfg := GlobalConfig()
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("subscriptions=%d, want 1", len(cfg.Subscriptions))
	}
	if len(cfg.SubscriptionPools) != 0 {
		t.Fatalf("导入不应创建/占用订阅池: %+v", cfg.SubscriptionPools)
	}
	if !cfg.SubscriptionPoolsInitialized {
		t.Fatal("导入后必须置位 SubscriptionPoolsInitialized，避免重启时被一次性迁移拉进默认池")
	}
	if _, err := cfg.activePoolSubscriptions(); err == nil {
		t.Fatal("未分配的订阅不应产生运行来源")
	}
}

// 回归：手动新建的订阅同样保持未分配（旧实现连池都不进，永远不会出现在配置里）。
func TestManualCreateDoesNotJoinAnyPool(t *testing.T) {
	useTestConfigDir(t)
	SetGlobalConfig(defaultConfig())

	d := &Daemon{}
	if err := d.createManualSubscription("手动节点"); err != nil {
		t.Fatal(err)
	}
	cfg := GlobalConfig()
	if len(cfg.SubscriptionPools) != 0 {
		t.Fatalf("手动新建不应创建订阅池: %+v", cfg.SubscriptionPools)
	}
	if !cfg.SubscriptionPoolsInitialized {
		t.Fatal("手动新建后必须置位 SubscriptionPoolsInitialized")
	}
}

// 回归：历史迁移只执行一次；之后重启不会把"刻意未分配"的订阅偷偷拉进默认池。
func TestSubscriptionPoolsMigrationRunsOnlyOnce(t *testing.T) {
	useTestConfigDir(t)
	cachePath := writeTestCache(t, "legacy-1")
	legacy := "subscriptions:\n" +
		"    - id: legacy-1\n      name: 旧订阅\n      url: https://old.example/sub\n      cache_file: " + cachePath + "\n" +
		"active_subscription: 0\n" +
		"proxy_mode: rule\ndefault_proxy_group: Auto\n"
	if err := os.WriteFile(configFilePath(), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	// 第一次加载：旧配置没有任何池，应迁移出一个默认池。
	first := LoadConfig()
	if len(first.SubscriptionPools) != 1 {
		t.Fatalf("旧配置应迁移出 1 个池: %+v", first.SubscriptionPools)
	}
	if !first.SubscriptionPoolsInitialized {
		t.Fatal("迁移后必须置位 SubscriptionPoolsInitialized")
	}
	if err := first.Flush(); err != nil {
		t.Fatal(err)
	}

	// 用户随后清空所有池（不再使用任何订阅池），再重启。
	cleared := LoadConfig()
	cleared.SubscriptionPools = nil
	cleared.SubscriptionPoolsInitialized = true
	if err := cleared.Flush(); err != nil {
		t.Fatal(err)
	}
	again := LoadConfig()
	if len(again.SubscriptionPools) != 0 {
		t.Fatalf("已完成的迁移不得重复执行: %+v", again.SubscriptionPools)
	}
}

// 回归：合并池的每个成员都影响运行，而不只是 ActiveMemberID。
func TestSubscriptionAffectsRuntimeMatrix(t *testing.T) {
	cfg := defaultConfig()
	cfg.SubscriptionPools = []SubscriptionPool{
		{ID: "merge", Name: "合并池", Mode: SubscriptionPoolModeMerge, Enabled: true,
			Members: []string{"m1", "m2"}, ActiveMemberID: "m1"},
		{ID: "fo", Name: "主备池", Mode: SubscriptionPoolModeFailover, Enabled: true,
			Members: []string{"f1", "f2"}, ActiveMemberID: "f1"},
		{ID: "off", Name: "禁用池", Mode: SubscriptionPoolModeMerge, Enabled: false,
			Members: []string{"d1"}},
	}
	cases := map[string]bool{
		"m1": true, // 合并池活动成员
		"m2": true, // 合并池非活动成员也必须触发重载
		"f1": true, // 主备池活动成员
		"f2": false,
		"d1": false, // 禁用池不影响运行
		"":   false,
		"xx": false,
	}
	for id, want := range cases {
		if got := cfg.subscriptionAffectsRuntime(id); got != want {
			t.Fatalf("subscriptionAffectsRuntime(%q)=%v, want %v", id, got, want)
		}
	}
}

// 回归：保存被禁用的池时，运行时应用失败必须回传给调用方，而不是静默 200。
func TestPoolSaveReportsApplyFailure(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "s1", Name: "甲", URL: "https://a.example", CacheFile: writeTestCache(t, "s1"), SourceType: SubscriptionSourceURL}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool-1", Name: "运行池", Mode: SubscriptionPoolModeMerge, Members: []string{"s1"}, ActiveMemberID: "s1", Enabled: true, RefreshInterval: 3600}}
	SetGlobalConfig(cfg)

	d := &Daemon{reconcileApply: func(reconcileRequest) ApplyReport {
		return ApplyReport{Applied: false, Stage: "generate", Err: "没有已启用的订阅池：1 个订阅池全部处于禁用状态"}
	}}
	body := `{"name":"运行池","mode":"merge","members":["s1"],"active_member_id":"s1","enabled":false,"refresh_interval":3600}`
	w := httptest.NewRecorder()
	d.handleSubscriptionPoolDetail(w, httptest.NewRequest(http.MethodPut, "/api/v1/subscription-pools/pool-1", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := w.Body.String()
	for _, want := range []string{`"applied":false`, `"apply_stage":"generate"`, "全部处于禁用状态"} {
		if !strings.Contains(got, want) {
			t.Fatalf("响应未暴露应用失败 %q: %s", want, got)
		}
	}
	if report, _, _ := d.LastApplyReport(); report.Applied {
		t.Fatal("daemon 应记录最近一次应用失败")
	}
}

// 回归：启用池的成员导入/刷新后必须触发重新应用（旧实现只在 ActiveMemberID 匹配时触发）。
func TestMergePoolImportTriggersApply(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "m1", Name: "合并甲", URL: "https://a.example/clash", CacheFile: writeTestCache(t, "m1"), SourceType: SubscriptionSourceURL},
		{ID: "m2", Name: "合并乙", URL: "https://b.example/clash", CacheFile: writeTestCache(t, "m2"), SourceType: SubscriptionSourceURL},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge,
		Members: []string{"m1", "m2"}, ActiveMemberID: "m1", Enabled: true, RefreshInterval: 3600}}
	SetGlobalConfig(cfg)

	applied := 0
	d := &Daemon{reconcileApply: func(reconcileRequest) ApplyReport { applied++; return ApplyReport{Applied: true} }}

	// 重复导入非活动成员 m2：内容相同也会走"已存在订阅"分支。
	body := `{"name":"合并乙","url":"https://b.example/clash","source_type":"content","content":"proxies:\n  - {name: m2-new, type: ss, server: 1.2.3.4, port: 1, cipher: aes-128-gcm, password: x}\n"}`
	w := httptest.NewRecorder()
	d.handleSubscriptions(w, httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", w.Code, w.Body.String())
	}
	if applied == 0 {
		t.Fatal("合并池非活动成员的内容变更必须触发配置重新应用")
	}
	got := GlobalConfig()
	if len(got.SubscriptionPools[0].Members) != 2 {
		t.Fatalf("重复导入不得改动池成员: %+v", got.SubscriptionPools[0].Members)
	}
}

// 回归：启动内核前必须按当前配置重新生成，不能继续跑过期的 config.yaml。
func TestStartRegeneratesStaleConfig(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "s1", Name: "甲", URL: "https://a.example", CacheFile: writeTestCache(t, "s1"), SourceType: SubscriptionSourceURL},
		{ID: "s2", Name: "乙", URL: "https://b.example", CacheFile: writeTestCache(t, "s2"), SourceType: SubscriptionSourceURL},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge,
		Members: []string{"s1", "s2"}, ActiveMemberID: "s1", Enabled: true, RefreshInterval: 3600}}
	SetGlobalConfig(cfg)

	// 预置一份只含 1 个 provider 的过期配置（模拟故障现场）。
	stalePath := filepath.Join(GetConfigDir(), "mihomo", MIHOMO_CONFIG_NAME)
	if err := os.MkdirAll(filepath.Dir(stalePath), 0700); err != nil {
		t.Fatal(err)
	}
	stale := "proxy-providers:\n    provider1:\n        path: /old/provider1.yaml\n        type: file\n"
	if err := os.WriteFile(stalePath, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{}
	if err := d.ensureRuntimeConfigFresh(&cfg); err != nil {
		t.Fatalf("ensureRuntimeConfigFresh: %v", err)
	}
	if count, err := configProviderCount(stalePath); err != nil || count != 2 {
		t.Fatalf("启动前应重新生成 2 个 provider，实际 %d (err=%v)", count, err)
	}
}

// 回归：没有可用来源时启动必须失败并说明原因，而不是静默使用旧配置。
func TestStartRefusesWhenNoRunnableSource(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{{ID: "s1", Name: "甲", URL: "https://a.example", CacheFile: writeTestCache(t, "s1"), SourceType: SubscriptionSourceURL}}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "运行池", Mode: SubscriptionPoolModeMerge,
		Members: []string{"s1"}, ActiveMemberID: "s1", Enabled: false}}

	d := &Daemon{}
	err := d.ensureRuntimeConfigFresh(&cfg)
	if err == nil {
		t.Fatal("全部池被禁用时启动前生成必须失败")
	}
	if !strings.Contains(err.Error(), "禁用") {
		t.Fatalf("错误信息应指出池被禁用: %v", err)
	}
}

// 回归：成员被跳过时必须记录原因，且可用计数与生成结果一致。
func TestPoolAvailabilityMatchesGeneratedProviders(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "ok1", Name: "可用甲", CacheFile: writeTestCache(t, "ok1")},
		{ID: "ok2", Name: "可用乙", CacheFile: writeTestCache(t, "ok2")},
		{ID: "off", Name: "禁用丙", Disabled: true, CacheFile: writeTestCache(t, "off")},
		{ID: "nocache", Name: "缺缓存丁", CacheFile: missing},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge, Enabled: true,
		Members: []string{"ok1", "ok2", "off", "nocache"}, ActiveMemberID: "ok1"}}

	avail := cfg.PoolAvailability(cfg.SubscriptionPools[0])
	if avail.Available != 2 || avail.Total != 4 {
		t.Fatalf("availability=%+v, want 2/4", avail)
	}
	if len(avail.Skipped) != 2 {
		t.Fatalf("skipped=%+v, want 2", avail.Skipped)
	}
	providers, _, groups, err := cfg.buildProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != avail.Available {
		t.Fatalf("provider=%d 与界面可用数 %d 不一致", len(providers), avail.Available)
	}
	if len(groups) == 0 || len(groups[0].Use) != avail.Available {
		t.Fatalf("策略组 use 列表与可用数不一致: %+v", groups)
	}
}
