package mihomotui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLogs 把结构化日志重定向到内存缓冲，测试结束后恢复原状。
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := InitConsoleLogger(&buf, "debug"); err != nil {
		t.Fatalf("InitConsoleLogger: %v", err)
	}
	t.Cleanup(func() {
		logMu.Lock()
		logW = nil
		logger = nil
		logMu.Unlock()
	})
	return &buf
}

// provider 名必须由订阅稳定 ID 推导：成员顺序变化不得改变同一个订阅的 provider 名，
// 否则 use 列表、provider API 路径与 cache.db 中的选择状态都会跟着漂移。
func TestProxyProviderNameStableAcrossOrder(t *testing.T) {
	a := SubscriptionMeta{ID: "aaaa1111"}
	b := SubscriptionMeta{ID: "bbbb2222"}

	first := proxyProviderName(a, 0)
	if first != proxyProviderName(a, 3) {
		t.Fatalf("同一订阅的顺序变化导致 provider 名漂移: %q vs %q", first, proxyProviderName(a, 3))
	}
	if first == proxyProviderName(b, 0) {
		t.Fatalf("不同订阅得到相同 provider 名: %q", first)
	}
	if !strings.HasPrefix(first, "provider-") {
		t.Fatalf("provider 名应以 provider- 前缀标识稳定命名: %q", first)
	}
	if got, want := len(first), len("provider-")+8; got != want {
		t.Fatalf("provider 名长度=%d，want %d（8 位短哈希）: %q", got, want, first)
	}
	// 缺 ID 的历史订阅继续使用位置编号，保证旧配置仍可生成。
	if got := proxyProviderName(SubscriptionMeta{}, 1); got != "provider2" {
		t.Fatalf("缺 ID 时应回退位置编号: %q", got)
	}
}

// 哈希前缀碰撞时不能静默覆盖：必须退化出唯一的 provider 名。
func TestProviderNamesForAvoidsCollision(t *testing.T) {
	logs := captureLogs(t)
	// 直接构造重复 ID（正常配置会被校验拦下），用于验证退化分支。
	subs := []SubscriptionMeta{{ID: "same-id"}, {ID: "same-id"}, {ID: "other-id"}}
	names := providerNamesFor(subs)
	if len(names) != 3 {
		t.Fatalf("names=%v", names)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" {
			t.Fatal("provider 名不能为空")
		}
		if seen[name] {
			t.Fatalf("provider 名重复，会导致 map 覆盖: %v", names)
		}
		seen[name] = true
	}
	if names[0] == names[1] {
		t.Fatalf("碰撞时未退化: %v", names)
	}
	if !strings.Contains(logs.String(), "哈希碰撞") {
		t.Fatalf("碰撞退化应记录告警: %s", logs.String())
	}
}

// provider 名只由订阅 ID 决定，与订阅在列表中的位置无关（除碰撞退化外）。
func TestProviderNamesForIsOrderIndependent(t *testing.T) {
	a := SubscriptionMeta{ID: "id-a"}
	b := SubscriptionMeta{ID: "id-b"}
	names := providerNamesFor([]SubscriptionMeta{a, b})
	reversed := providerNamesFor([]SubscriptionMeta{b, a})
	if names[0] != reversed[1] || names[1] != reversed[0] {
		t.Fatalf("命名不应依赖顺序: %v vs %v", names, reversed)
	}
}

func TestBuildProxyConfigUsesStableProviderNames(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "sub-a", Name: "甲", CacheFile: writeTestCache(t, "sub-a")},
		{ID: "sub-b", Name: "乙", CacheFile: writeTestCache(t, "sub-b")},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge, Enabled: true,
		Members: []string{"sub-a", "sub-b"}, ActiveMemberID: "sub-a"}}

	providers, _, groups, err := cfg.buildProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	wantA, wantB := proxyProviderName(cfg.Subscriptions[0], 0), proxyProviderName(cfg.Subscriptions[1], 1)
	if _, ok := providers[wantA]; !ok {
		t.Fatalf("providers 缺少 %q: %+v", wantA, providers)
	}
	if _, ok := providers[wantB]; !ok {
		t.Fatalf("providers 缺少 %q: %+v", wantB, providers)
	}
	if len(groups) == 0 {
		t.Fatal("缺少策略组")
	}
	got := groups[0].Use
	if len(got) != 2 || got[0] != wantA || got[1] != wantB {
		t.Fatalf("use 列表=%v，want [%s %s]", got, wantA, wantB)
	}

	// 打乱成员顺序后，同一个订阅的 provider 名必须保持不变（只是顺序不同）。
	cfg.SubscriptionPools[0].Members = []string{"sub-b", "sub-a"}
	providers2, _, groups2, err := cfg.buildProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{wantA, wantB} {
		if _, ok := providers2[name]; !ok {
			t.Fatalf("重排成员后 provider %q 消失，说明命名仍依赖位置", name)
		}
	}
	if groups2[0].Use[0] != wantB || groups2[0].Use[1] != wantA {
		t.Fatalf("use 列表应跟随成员顺序: %v", groups2[0].Use)
	}
}

// 额度同步使用的 provider 名必须与生成配置一致，否则运行中 provider 的额度永远同步不到。
func TestProviderMetadataSyncUsesSameProviderName(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "meta-1", Name: "甲", CacheFile: writeTestCache(t, "meta-1")},
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "池", Enabled: true,
		Members: []string{"meta-1"}, ActiveMemberID: "meta-1"}}
	SetGlobalConfig(cfg)

	active, err := GlobalConfig().activePoolSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active=%d", len(active))
	}
	// 生成配置用的名字与同步查询用的名字必须是同一个。
	providers, _, _, err := GlobalConfig().buildProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	name := proxyProviderName(active[0], 0)
	if _, ok := providers[name]; !ok {
		t.Fatalf("额度同步查询的 provider 名 %q 不在生成结果 %v 中", name, providers)
	}
}

func TestProviderProxyNamesReadsAndCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	content := "proxies:\n  - {name: 香港01, type: ss}\n  - {name: 日本02, type: ss}\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	names, err := readProviderProxyNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "香港01" || names[1] != "日本02" {
		t.Fatalf("names=%v", names)
	}

	// 内容变化后必须重新解析（缓存按大小+修改时间失效）。
	updated := "proxies:\n  - {name: 新加坡03, type: ss}\n"
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	names, err = readProviderProxyNames(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "新加坡03" {
		t.Fatalf("缓存未失效，仍返回旧节点名: %v", names)
	}

	// 非 Clash YAML 正文（如 URI 列表）解析失败，调用方应只是跳过检测。
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("ss://very-long-uri"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProviderProxyNames(bad); err == nil {
		t.Fatal("非法正文应返回解析错误，由调用方决定跳过")
	}
}

// 跨订阅同名节点必须告警（mihomo 按节点名区分，重名会被覆盖）。
func TestWarnDuplicateProviderNodes(t *testing.T) {
	useTestConfigDir(t)
	logs := captureLogs(t)

	mk := func(id, nodes string) string {
		t.Helper()
		path, _, err := writeSubscriptionCache(id, []byte(nodes))
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	first := mk("dup-a", "proxies:\n  - {name: 香港01, type: ss}\n  - {name: 日本02, type: ss}\n")
	second := mk("dup-b", "proxies:\n  - {name: 香港01, type: ss}\n  - {name: 美国03, type: ss}\n")
	third := mk("dup-c", "proxies:\n  - {name: 韩国04, type: ss}\n")

	warnDuplicateProviderNodes([]SubscriptionMeta{
		{ID: "dup-a", Name: "订阅甲", CacheFile: first},
		{ID: "dup-b", Name: "订阅乙", CacheFile: second},
		{ID: "dup-c", Name: "订阅丙", CacheFile: third},
	})

	out := logs.String()
	if !strings.Contains(out, "同名节点") || !strings.Contains(out, "香港01") {
		t.Fatalf("缺少重名节点告警: %s", out)
	}
	if !strings.Contains(out, "订阅甲") || !strings.Contains(out, "订阅乙") {
		t.Fatalf("告警应指明冲突的订阅: %s", out)
	}
	if strings.Contains(out, "日本02") || strings.Contains(out, "韩国04") || strings.Contains(out, "美国03") {
		t.Fatalf("非重名节点不应出现在告警中: %s", out)
	}
}

// 单个订阅或全部唯一时不产生告警，避免日志噪音。
func TestWarnDuplicateProviderNodesSilentWhenUnique(t *testing.T) {
	useTestConfigDir(t)
	logs := captureLogs(t)

	path, _, err := writeSubscriptionCache("solo", []byte("proxies:\n  - {name: 香港01, type: ss}\n"))
	if err != nil {
		t.Fatal(err)
	}
	warnDuplicateProviderNodes([]SubscriptionMeta{{ID: "solo", CacheFile: path}})
	if out := logs.String(); strings.Contains(out, "同名节点") {
		t.Fatalf("单订阅不应告警: %s", out)
	}

	other, _, err := writeSubscriptionCache("solo2", []byte("proxies:\n  - {name: 日本02, type: ss}\n"))
	if err != nil {
		t.Fatal(err)
	}
	warnDuplicateProviderNodes([]SubscriptionMeta{
		{ID: "solo", CacheFile: path},
		{ID: "solo2", CacheFile: other},
	})
	if out := logs.String(); strings.Contains(out, "同名节点") {
		t.Fatalf("节点名唯一时不应告警: %s", out)
	}
}

// 生成配置时确实会触发重名检测（而不只是单独调用工具函数）。
func TestGenerateMihomoConfigWarnsOnDuplicateNodes(t *testing.T) {
	useTestConfigDir(t)
	logs := captureLogs(t)

	cfg := defaultConfig()
	cfg.Subscriptions = []SubscriptionMeta{
		{ID: "g-a", Name: "订阅甲", URL: "https://a.example", SourceType: SubscriptionSourceURL},
		{ID: "g-b", Name: "订阅乙", URL: "https://b.example", SourceType: SubscriptionSourceURL},
	}
	for i := range cfg.Subscriptions {
		path, _, err := writeSubscriptionCache(cfg.Subscriptions[i].ID,
			[]byte("proxies:\n  - {name: 共同节点, type: ss}\n"))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Subscriptions[i].CacheFile = path
	}
	cfg.SubscriptionPools = []SubscriptionPool{{ID: "pool", Name: "合并池", Mode: SubscriptionPoolModeMerge, Enabled: true,
		Members: []string{"g-a", "g-b"}, ActiveMemberID: "g-a"}}

	if err := cfg.GenerateMihomoConfig(); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, "同名节点") || !strings.Contains(out, "共同节点") {
		t.Fatalf("生成配置时应告警重名节点: %s", out)
	}
	// 告警不得修改用户节点名。
	data, err := os.ReadFile(cfg.MihomoConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider-") {
		t.Fatalf("生成结果应使用稳定 provider 名: %s", data)
	}
}
