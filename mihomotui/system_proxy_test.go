package mihomotui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupSystemProxyHome 把 HOME 指向临时目录并准备三个 shell 配置文件。
func setupSystemProxyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{".bashrc", ".zshrc", ".profile"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("# existing\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSetSystemProxyEnvWritesAndClears 开启写入标记块、关闭彻底清除，且不影响原有内容。
func TestSetSystemProxyEnvWritesAndClears(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)

	cfg := defaultConfig()
	cfg.Mihomo.MixedPort = 7892
	cfg.Mihomo.SOCKS5Port = 7891

	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatalf("SetSystemProxyEnv(true) error = %v", err)
	}
	for _, name := range []string{".bashrc", ".zshrc", ".profile"} {
		content := readFile(t, filepath.Join(home, name))
		if !strings.Contains(content, sysProxyBlockStart) {
			t.Fatalf("%s 缺少起始标记", name)
		}
		if !strings.Contains(content, "export http_proxy=http://127.0.0.1:7892") {
			t.Fatalf("%s 未写入 http 代理", name)
		}
		if !strings.Contains(content, "export ALL_PROXY=socks5://127.0.0.1:7891") {
			t.Fatalf("%s 未写入 socks 代理", name)
		}
		if !strings.Contains(content, "# existing") {
			t.Fatalf("%s 原有内容被破坏", name)
		}
	}

	if err := cfg.SetSystemProxyEnv(false); err != nil {
		t.Fatalf("SetSystemProxyEnv(false) error = %v", err)
	}
	for _, name := range []string{".bashrc", ".zshrc", ".profile"} {
		content := readFile(t, filepath.Join(home, name))
		if strings.Contains(content, sysProxyBlockStart) {
			t.Fatalf("%s 关闭后仍残留标记块", name)
		}
		if !strings.Contains(content, "# existing") {
			t.Fatalf("%s 关闭后原有内容被破坏", name)
		}
	}
}

// TestSetSystemProxyEnvIdempotent 内容一致时不重复写文件（避免无谓 mtime 变化）。
func TestSetSystemProxyEnvIdempotent(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)
	cfg := defaultConfig()

	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".bashrc")
	before := readFile(t, path)
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatal(err)
	}
	after := readFile(t, path)
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("重复开启改变了文件内容")
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("重复开启不应重写文件（mtime 变化）")
	}
	if strings.Count(after, sysProxyBlockStart) != 1 {
		t.Fatal("重复开启产生了多个标记块")
	}
}

// TestSetSystemProxyEnvUpdatesChangedPort 端口变化时原地替换旧块，不产生重复块。
func TestSetSystemProxyEnvUpdatesChangedPort(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)

	cfg := defaultConfig()
	cfg.Mihomo.MixedPort = 7892
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatal(err)
	}

	cfg.Mihomo.MixedPort = 8888
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatal(err)
	}

	content := readFile(t, filepath.Join(home, ".bashrc"))
	if strings.Count(content, sysProxyBlockStart) != 1 {
		t.Fatalf("端口变更后出现重复块:\n%s", content)
	}
	if !strings.Contains(content, "http://127.0.0.1:8888") {
		t.Fatalf("端口未更新:\n%s", content)
	}
	if strings.Contains(content, "http://127.0.0.1:7892") {
		t.Fatalf("旧端口仍残留:\n%s", content)
	}
}

// TestReconcileSystemProxyEnvFollowsPreference 对账按偏好补齐或清理注入块。
func TestReconcileSystemProxyEnvFollowsPreference(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	// 偏好关闭：对账不应写入任何块。
	if err := SetSystemProxyPreference(false); err != nil {
		t.Fatal(err)
	}
	if changed, err := ReconcileSystemProxyEnv(); err != nil || changed {
		t.Fatalf("偏好关闭时对账 changed=%v err=%v，应为 false/nil", changed, err)
	}
	if strings.Contains(readFile(t, filepath.Join(home, ".bashrc")), sysProxyBlockStart) {
		t.Fatal("偏好关闭时不应注入")
	}

	// 偏好开启但文件被清空（模拟被 ExecStop 误删）：对账应补齐。
	if err := SetSystemProxyPreference(true); err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileSystemProxyEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("偏好开启时对账应补齐注入块")
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".bashrc")), sysProxyBlockStart) {
		t.Fatal("对账后仍缺少注入块")
	}

	// 再次对账：内容一致，不应修改文件。
	changed, err = ReconcileSystemProxyEnv()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("内容一致时对账不应产生写入")
	}
}

// TestStripSystemProxyBlockKeepsUnterminatedContent 起始标记缺少结束标记时原样返回，
// 避免把标记之后的用户内容误删。
func TestStripSystemProxyBlockKeepsUnterminatedContent(t *testing.T) {
	content := "# before\n" + sysProxyBlockStart + "\nexport http_proxy=http://127.0.0.1:7892\n# after\n"
	if got := stripSystemProxyBlock(content); got != content {
		t.Fatalf("未配对标记应原样返回，got:\n%s", got)
	}
}

// TestRemoveBlockFromFilePreservesUserContent 删除标记块只影响块本身，保留其余内容。
func TestRemoveBlockFromFilePreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".bashrc")
	cfg := defaultConfig()
	original := "export PATH=$PATH:/opt/bin\n" + buildSystemProxyBlock(&cfg) + "alias ll='ls -l'\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	modified, err := removeBlockFromFile(path, sysProxyBlockStart, sysProxyBlockEnd)
	if err != nil {
		t.Fatal(err)
	}
	if !modified {
		t.Fatal("应报告文件被修改")
	}
	got := readFile(t, path)
	if strings.Contains(got, sysProxyBlockStart) {
		t.Fatalf("标记块未清除:\n%s", got)
	}
	if !strings.Contains(got, "export PATH=$PATH:/opt/bin") || !strings.Contains(got, "alias ll='ls -l'") {
		t.Fatalf("用户内容被破坏:\n%s", got)
	}

	// 再次删除：无块可删，不应报告修改。
	modified, err = removeBlockFromFile(path, sysProxyBlockStart, sysProxyBlockEnd)
	if err != nil {
		t.Fatal(err)
	}
	if modified {
		t.Fatal("无标记块时不应报告修改")
	}
}

// TestSetSystemProxyEnvCreatesProfileWhenNoShellFiles 三个 shell 文件都不存在时，
// 至少创建 ~/.profile 承载注入，避免"偏好开启但哪里都没写"的静默失效。
func TestSetSystemProxyEnvCreatesProfileWhenNoShellFiles(t *testing.T) {
	useTestConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := defaultConfig()
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatalf("SetSystemProxyEnv(true) error = %v", err)
	}
	content := readFile(t, filepath.Join(home, ".profile"))
	if !strings.Contains(content, sysProxyBlockStart) {
		t.Fatalf("~/.profile 未写入注入块:\n%s", content)
	}
}

// TestShellConfigFilesIncludesBashProfile 登录 shell 若存在 .bash_profile，
// 必须一并注入，否则 SSH 登录读不到 .profile 里的块。
func TestShellConfigFilesIncludesBashProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// 没有 .bash_profile 时，只有常规三项（外加 .bashrc）。
	files := shellConfigFiles()
	if containsString(files, filepath.Join(home, ".bash_profile")) {
		t.Fatal("不存在 .bash_profile 时不应纳入")
	}

	// 创建 .bash_profile 后必须纳入。
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("# login\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files = shellConfigFiles()
	if !containsString(files, filepath.Join(home, ".bash_profile")) {
		t.Fatal("存在 .bash_profile 时必须纳入注入目标")
	}
}

// TestSetSystemProxyEnvWritesBashProfile 注入要真正落到登录 shell 入口文件上。
func TestSetSystemProxyEnvWritesBashProfile(t *testing.T) {
	useTestConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	loginFile := filepath.Join(home, ".bash_profile")
	if err := os.WriteFile(loginFile, []byte("# login\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatalf("SetSystemProxyEnv(true) error = %v", err)
	}
	if !strings.Contains(readFile(t, loginFile), sysProxyBlockStart) {
		t.Fatal(".bash_profile 未注入，SSH 登录读不到代理变量")
	}
}

// TestDiagnoseReportsMissingLoginEntryBlock 登录入口缺块时诊断必须指出，
// 而不是笼统地说"没生效"。
func TestDiagnoseReportsMissingLoginEntryBlock(t *testing.T) {
	useTestConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	// .bash_profile 存在但没块；.bashrc 有块 → 交互式 shell 有、登录 shell 没有。
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("# login\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(buildSystemProxyBlock(&cfg)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetSystemProxyPreference(true); err != nil {
		t.Fatal(err)
	}

	diag := DiagnoseSystemProxy()
	var found bool
	for _, note := range diag.Notes {
		if strings.Contains(note, "登录 shell 入口") {
			found = true
		}
	}
	if !found {
		t.Fatalf("诊断未指出登录入口缺块，notes=%v", diag.Notes)
	}
}

// TestDiagnoseReportsPortNotListening 端口未监听时诊断要给出明确结论。
func TestDiagnoseReportsPortNotListening(t *testing.T) {
	useTestConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	if err := SetSystemProxyPreference(true); err != nil {
		t.Fatal(err)
	}
	// 指向一个必定未监听的端口。
	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.Mihomo.MixedPort = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	diag := DiagnoseSystemProxy()
	if diag.PortListening {
		t.Fatal("端口 1 不应被判定为监听中")
	}
	var found bool
	for _, note := range diag.Notes {
		if strings.Contains(note, "未监听") {
			found = true
		}
	}
	if !found {
		t.Fatalf("诊断未指出端口未监听，notes=%v", diag.Notes)
	}
}

// TestSystemProxyBlockInsertedBeforeNonInteractiveGuard Ubuntu 的 .bashrc 开头有
// 「非交互式直接 return」守卫；块必须插在它之前，否则 `ssh host 'cmd'` 读不到代理。
func TestSystemProxyBlockInsertedBeforeNonInteractiveGuard(t *testing.T) {
	useTestConfigDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	bashrc := filepath.Join(home, ".bashrc")
	original := "# ~/.bashrc\n\n# If not running interactively, don't do anything\ncase $- in\n    *i*) ;;\n      *) return;;\nesac\n\nalias ll='ls -l'\n"
	if err := os.WriteFile(bashrc, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultConfig()
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatalf("SetSystemProxyEnv(true) error = %v", err)
	}

	content := readFile(t, bashrc)
	blockIdx := strings.Index(content, sysProxyBlockStart)
	guardIdx := strings.Index(content, "case $- in")
	if blockIdx < 0 {
		t.Fatal("未写入注入块")
	}
	if guardIdx < 0 {
		t.Fatal("守卫被破坏")
	}
	if blockIdx > guardIdx {
		t.Fatalf("注入块位于非交互式 return 守卫之后，SSH 非交互命令读不到:\n%s", content)
	}
	if !strings.Contains(content, "alias ll='ls -l'") {
		t.Fatal("用户原有内容被破坏")
	}
}

// TestBashrcNonInteractiveReturnIndex 守卫识别：命中返回偏移，无守卫返回 -1。
func TestBashrcNonInteractiveReturnIndex(t *testing.T) {
	guarded := "case $- in\n    *i*) ;;\n      *) return;;\nesac\n"
	if idx := bashrcNonInteractiveReturnIndex(guarded); idx != 0 {
		t.Fatalf("应识别守卫并返回偏移 0，got %d", idx)
	}

	noReturn := "case \"$x\" in\n  a) echo a ;;\nesac\n"
	if idx := bashrcNonInteractiveReturnIndex(noReturn); idx != -1 {
		t.Fatalf("无 return 的 case 不应命中，got %d", idx)
	}

	if idx := bashrcNonInteractiveReturnIndex("alias ll='ls -l'\n"); idx != -1 {
		t.Fatalf("无守卫文件应返回 -1，got %d", idx)
	}
}

// TestContainsReturnStatement return 语句识别不能误判标识符。
func TestContainsReturnStatement(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"return", true},
		{"*) return;;", true},
		{";; *) return 0 ;;", true},
		{"  return", true},
		{"return_val=1", false},
		{"myreturn=1", false},
		{"echo return", true}, // 宽松：行内出现独立 return 即认为命中，宁可不漏判
	}
	for _, c := range cases {
		if got := containsReturnStatement(c.line); got != c.want {
			t.Fatalf("containsReturnStatement(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// TestClientPreferencesFollowSudoUser root 代 sudo 用户操作时，偏好应写入该用户的
// 配置目录，而不是 /root。否则普通用户下次启动读不到偏好，会把注入块当成"应清理"。
func TestClientPreferencesFollowSudoUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("需要 root 才能模拟 sudo 场景")
	}
	useTestConfigDir(t)
	t.Setenv("SUDO_USER", "nobody")

	home := homeDirOfUser("nobody")
	if home == "" {
		t.Skip("系统中没有 nobody 用户")
	}
	want := filepath.Join(home, ".config", ConfigDirName, ClientPreferencesFileName)
	if got := preferencesFilePath(); got != want {
		t.Fatalf("preferencesFilePath() = %s, want %s", got, want)
	}
}

// TestDisableSystemProxyOnKernelStop 停内核时应把偏好与注入块一起关掉，
// 避免新开终端拿到指向死端口的代理变量。
func TestDisableSystemProxyOnKernelStop(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	cfg := defaultConfig()
	if err := cfg.SetSystemProxyEnv(true); err != nil {
		t.Fatal(err)
	}
	if err := SetSystemProxyPreference(true); err != nil {
		t.Fatal(err)
	}

	disabled, err := DisableSystemProxyOnKernelStop()
	if err != nil {
		t.Fatalf("DisableSystemProxyOnKernelStop() error = %v", err)
	}
	if !disabled {
		t.Fatal("原本开启时应报告已关闭")
	}
	if GetSystemProxyPreference() {
		t.Fatal("偏好应被置为关闭")
	}
	if strings.Contains(readFile(t, filepath.Join(home, ".bashrc")), sysProxyBlockStart) {
		t.Fatal("注入块应被清除")
	}

	// 已经是关闭状态：不应重复报告关闭。
	disabled, err = DisableSystemProxyOnKernelStop()
	if err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Fatal("已是关闭状态时不应报告已关闭")
	}
}

// TestReconcileIfUsableSkipsInjectionWhenPortDead 端口未监听时安全包装必须拒绝注入，
// 否则会把指向死端口的代理变量写回 rc 文件，让新开终端断网。
func TestReconcileIfUsableSkipsInjectionWhenPortDead(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	// 指向一个必定未监听的端口。
	if _, err := UpdateGlobalConfig(func(c *Config) error {
		c.Mihomo.MixedPort = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetSystemProxyPreference(true); err != nil {
		t.Fatal(err)
	}

	changed, skipped, err := ReconcileSystemProxyEnvIfUsable()
	if err != nil {
		t.Fatal(err)
	}
	if !skipped {
		t.Fatal("端口未监听时应跳过注入")
	}
	if changed {
		t.Fatal("端口未监听时不应修改文件")
	}
	if strings.Contains(readFile(t, filepath.Join(home, ".bashrc")), sysProxyBlockStart) {
		t.Fatal("端口未监听时不应写入注入块")
	}
}

// TestReconcileIfUsableClearsWhenPreferenceOff 偏好关闭时安全包装应清理残留块，
// 且不受端口状态影响。
func TestReconcileIfUsableClearsWhenPreferenceOff(t *testing.T) {
	useTestConfigDir(t)
	home := setupSystemProxyHome(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)

	// 先写入一个残留块。
	cfg := defaultConfig()
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(buildSystemProxyBlock(&cfg)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetSystemProxyPreference(false); err != nil {
		t.Fatal(err)
	}

	changed, skipped, err := ReconcileSystemProxyEnvIfUsable()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("残留块应被清理")
	}
	if skipped {
		t.Fatal("偏好关闭时不应跳过（清理不受端口影响）")
	}
	if strings.Contains(readFile(t, filepath.Join(home, ".bashrc")), sysProxyBlockStart) {
		t.Fatal("残留块未被清除")
	}
}

// TestSystemProxyClearHintDoesNotSuggestSource 关闭后的提示不能建议 source：
// 注入块已删除，文件里没有 unset，source 无法清掉已运行 shell 里的旧变量。
func TestSystemProxyClearHintDoesNotSuggestSource(t *testing.T) {
	hint := SystemProxyClearHint()
	if strings.Contains(hint, "source ~/.bashrc") {
		t.Fatalf("关闭提示不应建议 source（无法清除已有变量）:\n%s", hint)
	}
	if !strings.Contains(hint, "unset http_proxy") {
		t.Fatalf("关闭提示应给出 unset 命令:\n%s", hint)
	}
}

// TestSystemProxySessionHintSuggestsSource 开启时的提示应建议 source（块里有 export，source 有效）。
func TestSystemProxySessionHintSuggestsSource(t *testing.T) {
	hint := SystemProxySessionHint()
	if !strings.Contains(hint, "source ~/.bashrc") {
		t.Fatalf("开启提示应建议 source:\n%s", hint)
	}
}

// TestProxyPortListening 未监听的端口应返回 false。
func TestProxyPortListening(t *testing.T) {
	// 端口 1 上不会有服务监听。
	if ProxyPortListening(1) {
		t.Fatal("端口 1 不应被判定为监听中")
	}
	if ProxyPortListening(0) {
		t.Fatal("非法端口不应被判定为监听中")
	}
}

// TestSystemProxyPortsFallback 混合端口缺失时回退 HTTP 端口，再回退默认值。
func TestSystemProxyPortsFallback(t *testing.T) {
	cfg := defaultConfig()
	cfg.Mihomo.MixedPort = 0
	cfg.Mihomo.HTTPPort = 8080
	cfg.Mihomo.SOCKS5Port = 0
	httpPort, socksPort := SystemProxyPorts(&cfg)
	if httpPort != 8080 {
		t.Fatalf("httpPort = %d, want 8080", httpPort)
	}
	if socksPort != DefaultSOCKS5Port {
		t.Fatalf("socksPort = %d, want %d", socksPort, DefaultSOCKS5Port)
	}
}
