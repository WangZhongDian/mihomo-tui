package mihomotui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTestConfigDir(t *testing.T) {
	t.Helper()
	oldDir := GetCustomConfigDir()
	oldCfg := *GlobalConfig()
	configMu.Lock()
	customConfigDir = t.TempDir()
	configMu.Unlock()
	SetGlobalConfig(defaultConfig())
	t.Cleanup(func() {
		configMu.Lock()
		customConfigDir = oldDir
		configMu.Unlock()
		SetGlobalConfig(oldCfg)
	})
}

func TestRemoveSubscriptionKeepsActiveSubscriptionConsistent(t *testing.T) {
	useTestConfigDir(t)
	cfg := Config{
		Subscriptions:      []SubscriptionMeta{{Name: "one"}, {Name: "two"}, {Name: "three"}},
		ActiveSubscription: 1,
	}
	if err := cfg.RemoveSubscription("one"); err != nil {
		t.Fatalf("RemoveSubscription() error = %v", err)
	}
	if got, want := cfg.ActiveSubscription, 0; got != want || cfg.Subscriptions[got].Name != "two" {
		t.Fatalf("active after deleting preceding subscription = %d (%s), want %d (two)", got, cfg.Subscriptions[got].Name, want)
	}
	if err := cfg.RemoveSubscription("two"); err != nil {
		t.Fatalf("RemoveSubscription() error = %v", err)
	}
	if got, want := cfg.ActiveSubscription, 0; got != want || cfg.Subscriptions[got].Name != "three" {
		t.Fatalf("active after deleting active subscription = %d (%s), want %d (three)", got, cfg.Subscriptions[got].Name, want)
	}
	if err := cfg.RemoveSubscription("three"); err != nil {
		t.Fatalf("RemoveSubscription() error = %v", err)
	}
	if got := cfg.ActiveSubscription; got != -1 {
		t.Fatalf("active after deleting final subscription = %d, want -1", got)
	}
}

func TestRedactURLRemovesCredentialsAndQuery(t *testing.T) {
	raw := "https://token:secret@example.com/subscription/path?token=abc#fragment"
	if got, want := RedactURL(raw), "https://example.com/subscription/path"; got != want {
		t.Fatalf("RedactURL() = %q, want %q", got, want)
	}
	if got := RedactURL("://bad"); got != "[invalid-url]" {
		t.Fatalf("RedactURL(invalid) = %q", got)
	}
}

func TestImportAndRefreshSubscriptionRecordsMetadataAndError(t *testing.T) {
	useTestConfigDir(t)
	var content = "ss://example-node"
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=1073741824; download=2147483648; total=4294967296")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	d := &Daemon{}
	if err := d.importSubscription("", server.URL+"/my-sub"); err != nil {
		t.Fatalf("importSubscription() error = %v", err)
	}
	cfg := GlobalConfig()
	if len(cfg.Subscriptions) != 1 {
		t.Fatalf("subscription count = %d, want 1", len(cfg.Subscriptions))
	}
	sub := cfg.Subscriptions[0]
	if sub.ID == "" || sub.Name != "my-sub" || sub.LastSuccessAt == "" || sub.LastError != "" {
		t.Fatalf("unexpected imported subscription metadata: %+v", sub)
	}
	// 路由支持稳定 ID，显示名称仍保留兼容性；重命名后刷新不应依赖旧名称。
	GlobalConfig().Subscriptions[0].Name = "renamed-sub"
	if err := d.refreshSubscription(sub.ID); err != nil {
		t.Fatalf("refreshSubscription(by ID) error = %v", err)
	}
	sub = GlobalConfig().Subscriptions[0]
	if sub.Name != "renamed-sub" || sub.LastError != "" {
		t.Fatalf("refresh by stable ID did not preserve renamed subscription: %+v", sub)
	}
	if sub.UsedGB != 3 || sub.TotalGB != 4 {
		t.Fatalf("subscription usage = %.1f / %.1f GiB, want 3 / 4", sub.UsedGB, sub.TotalGB)
	}
	if err := d.importSubscription("", server.URL+"/my-sub"); err != nil {
		t.Fatalf("duplicate import error = %v", err)
	}
	if got := GlobalConfig().Subscriptions; len(got) != 1 || got[0].ID != sub.ID || got[0].Name != "renamed-sub" {
		t.Fatalf("duplicate import did not preserve stable subscription: %+v", got)
	}

	status = http.StatusBadGateway
	if err := d.refreshSubscription(sub.ID); err == nil {
		t.Fatal("refreshSubscription() unexpectedly succeeded")
	}
	updated := GlobalConfig().Subscriptions[0]
	if !strings.Contains(updated.LastError, "状态码: 502") {
		t.Fatalf("LastError = %q, want HTTP failure", updated.LastError)
	}
	if updated.LastSuccessAt != sub.LastSuccessAt {
		t.Fatalf("LastSuccessAt changed after failed refresh: %q -> %q", sub.LastSuccessAt, updated.LastSuccessAt)
	}
}

func TestImportAndRefreshRuleProviderRecordsMetadataAndError(t *testing.T) {
	useTestConfigDir(t)
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("payload:\n  - DOMAIN-SUFFIX,example.com"))
	}))
	defer server.Close()

	d := &Daemon{}
	if err := d.importRuleProvider(RuleProviderImportRequest{Name: "demo-rules", URL: server.URL, Behavior: "domain", Format: "yaml"}); err != nil {
		t.Fatalf("importRuleProvider() error = %v", err)
	}
	cfg := GlobalConfig()
	if len(cfg.RuleProviderSubscriptions) != 1 {
		t.Fatalf("rule provider count = %d, want 1", len(cfg.RuleProviderSubscriptions))
	}
	rp := cfg.RuleProviderSubscriptions[0]
	if rp.LastSuccessAt == "" || rp.LastError != "" || rp.Behavior != "domain" {
		t.Fatalf("unexpected imported rule provider: %+v", rp)
	}

	status = http.StatusBadGateway
	if err := d.refreshRuleProvider(rp.Name); err == nil {
		t.Fatal("refreshRuleProvider() unexpectedly succeeded")
	}
	updated := GlobalConfig().RuleProviderSubscriptions[0]
	if updated.LastFailureAt == "" || !strings.Contains(updated.LastError, "状态码: 502") {
		t.Fatalf("rule provider refresh error state not recorded: %+v", updated)
	}
}

func TestValidateSubscriptionContentRejectsGarbage(t *testing.T) {
	if err := validateSubscriptionContent([]byte("<html>not a subscription</html>")); err == nil {
		t.Fatal("validateSubscriptionContent() accepted HTML garbage")
	}
	if err := validateSubscriptionContent([]byte("ss://example-node")); err != nil {
		t.Fatalf("validateSubscriptionContent() rejected URI subscription: %v", err)
	}
	if err := validateSubscriptionContent([]byte("proxies:\n  - name: demo")); err != nil {
		t.Fatalf("validateSubscriptionContent() rejected Clash YAML: %v", err)
	}
	if err := validateSubscriptionContent([]byte("aGVsbG8=")); err == nil {
		t.Fatal("validateSubscriptionContent() accepted arbitrary Base64 garbage")
	}
}

func TestDownloadFileIsAtomic(t *testing.T) {
	useTestConfigDir(t)
	status := http.StatusOK
	body := "new-resource"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	dst := filepath.Join(t.TempDir(), "resource.dat")
	if err := os.WriteFile(dst, []byte("old-resource"), 0600); err != nil {
		t.Fatal(err)
	}
	status = http.StatusBadGateway
	if err := downloadFile(server.URL, dst, nil); err == nil {
		t.Fatal("downloadFile() unexpectedly succeeded")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-resource" {
		t.Fatalf("failed download overwrote destination: %q", got)
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains after failed download: %v", err)
	}

	status = http.StatusOK
	if err := downloadFile(server.URL, dst, nil); err != nil {
		t.Fatalf("downloadFile() error = %v", err)
	}
	got, err = os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("successful download = %q, want %q", got, body)
	}
}

func TestSubscriptionNetworkErrorsDoNotLeakCredentialURL(t *testing.T) {
	oldClient := subscriptionHTTPClient
	defer func() { subscriptionHTTPClient = oldClient }()
	secretURL := "https://user:password@example.invalid/sub?token=very-secret"
	subscriptionHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Get %q: dial failed", secretURL)
	})}

	if _, err := fetchSubscription(secretURL); err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "very-secret") {
		t.Fatalf("subscription fetch error leaked credential URL: %v", err)
	}
	if _, err := fetchRuleProvider(secretURL); err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "very-secret") {
		t.Fatalf("rule provider fetch error leaked credential URL: %v", err)
	}
	if got := RedactURLInText(fmt.Sprintf("Get %q: dial failed", secretURL)); strings.Contains(got, "password") || strings.Contains(got, "very-secret") {
		t.Fatalf("RedactURLInText leaked credential URL: %s", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSensitiveConfigFilesAndConfigResponseAreProtected(t *testing.T) {
	useTestConfigDir(t)
	cfg := defaultConfig()
	cfg.Mihomo.Secret = "super-secret"
	cfg.Subscriptions = []SubscriptionMeta{{ID: "sub-1", Name: "demo", URL: "https://example.com/sub"}}
	cfg.ActiveSubscription = 0
	if err := cfg.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	info, err := os.Stat(configFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0600); got != want {
		t.Fatalf("config file permission = %04o, want %04o", got, want)
	}
	if err := cfg.GenerateMihomoConfig(); err != nil {
		t.Fatalf("GenerateMihomoConfig() error = %v", err)
	}
	info, err = os.Stat(cfg.MihomoConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0600); got != want {
		t.Fatalf("mihomo config permission = %04o, want %04o", got, want)
	}

	SetGlobalConfig(cfg)
	d := &Daemon{}
	recorder := httptest.NewRecorder()
	d.handleConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /config status = %d, want 200", recorder.Code)
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Config Config `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Data.Config.Mihomo.Secret != "" {
		t.Fatalf("regular config response leaked secret: %+v", response)
	}

	credentialsRecorder := httptest.NewRecorder()
	d.handleMihomoAPICredentials(credentialsRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/mihomo/api-credentials", nil))
	if credentialsRecorder.Code != http.StatusOK {
		t.Fatalf("GET /mihomo/api-credentials status = %d, want 200", credentialsRecorder.Code)
	}
	var credentials APIResponse
	if err := json.Unmarshal(credentialsRecorder.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	values, err := unmarshalData[map[string]string](&credentials)
	if err != nil {
		t.Fatal(err)
	}
	if values["secret"] != "super-secret" {
		t.Fatalf("credential endpoint secret = %q", values["secret"])
	}
}

func TestSocketPermissionErrorIsPreservedAndActionable(t *testing.T) {
	err := ipcPermissionError("/var/run/mihomo-tui/daemon.sock", os.ErrPermission)
	if !errors.Is(err, ErrIPCPermissionDenied) || !IsIPCPermissionError(err) {
		t.Fatalf("IPC permission error was not classified: %v", err)
	}
	for _, want := range []string{"权限不足", "grant_operator", "重新登录", "newgrp mihomo-tui"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("permission error %q does not contain %q", err, want)
		}
	}
	if isSocketPermissionError(os.ErrNotExist) {
		t.Fatal("nonexistent socket was misclassified as permission error")
	}
	if !isSocketPermissionError(os.ErrPermission) {
		t.Fatal("EACCES socket error was not classified as permission error")
	}
}

func TestIPCHTTPForbiddenIsClassifiedAsPermissionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, fmt.Errorf("无权访问 IPC 服务"))
	}))
	defer server.Close()

	client := &IPCClient{client: server.Client(), baseURL: server.URL}
	_, err := client.request(http.MethodGet, "/api/v1/config", nil, nil)
	if !errors.Is(err, ErrIPCPermissionDenied) || !IsIPCPermissionError(err) {
		t.Fatalf("HTTP 403 was not classified as IPC permission error: %v", err)
	}
	if !strings.Contains(err.Error(), "无权访问 IPC 服务") {
		t.Fatalf("permission error lost server message: %v", err)
	}
}

func TestPrivateIPCSocketPermissionsAndRootFailClosed(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "daemon.sock")
	auth := &ipcAuthorizer{runsAsRoot: false, ownerUID: uint32(os.Geteuid())}
	if err := auth.configureSocketDirectory(dir); err != nil {
		t.Fatalf("configureSocketDirectory() error = %v", err)
	}
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := auth.configureSocketPermissions(sock); err != nil {
		t.Fatalf("configureSocketPermissions() error = %v", err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, sock: 0600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("permission for %s = %04o, want %04o", path, got, want)
		}
	}
	rootAuth := &ipcAuthorizer{runsAsRoot: true, hasGroups: false}
	if err := rootAuth.configureSocketDirectory(t.TempDir()); err == nil {
		t.Fatal("root IPC authorization unexpectedly accepted missing groups")
	}
}

func TestReadOnlyIPCExcludesSensitiveConfigurationAndCredentials(t *testing.T) {
	for _, endpoint := range []string{
		"/api/v1/config",
		"/api/v1/subscriptions",
		"/api/v1/rule-providers",
		"/api/v1/mihomo/api-credentials",
		"/api/v1/mihomo/external-resources",
	} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		if isIPCReadOnlyRequest(req) {
			t.Fatalf("read-only IPC unexpectedly permits sensitive endpoint %s", endpoint)
		}
	}
	for _, endpoint := range []string{
		"/api/v1/ping",
		"/api/v1/daemon/info",
		"/api/v1/mihomo/status",
		"/api/v1/mihomo/version",
	} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		if !isIPCReadOnlyRequest(req) {
			t.Fatalf("read-only IPC unexpectedly rejects status endpoint %s", endpoint)
		}
	}
}

func TestIPCAuthorizationClassifiesSensitiveEndpoints(t *testing.T) {
	requests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/api/v1/config", true},
		{http.MethodPost, "/api/v1/mihomo/start", true},
		{http.MethodPost, "/api/v1/mihomo/upgrade", true},
		{http.MethodPost, "/api/v1/daemon/shutdown", true},
		{http.MethodGet, "/api/v1/config", false},
		{http.MethodPut, "/api/v1/subscriptions/demo", false},
	}
	for _, tc := range requests {
		t.Run(fmt.Sprintf("%s_%s", tc.method, strings.ReplaceAll(tc.path, "/", "_")), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := isIPCRootOnlyRequest(req); got != tc.want {
				t.Fatalf("isIPCRootOnlyRequest(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}
