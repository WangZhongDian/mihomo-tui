package mihomotui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDelayEndpointsEscapeProxyAndGroupNames(t *testing.T) {
	useTestConfigDir(t)
	cfg := *GlobalConfig()
	cfg.MihomoRunningVersion = "1.19.27"
	SetGlobalConfig(cfg)

	const name = "香港 #1/测试"
	const wantPath = "/proxies/%E9%A6%99%E6%B8%AF%20%231%2F%E6%B5%8B%E8%AF%95/delay"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != wantPath {
			t.Errorf("escaped path = %q, want %q", got, wantPath)
		}
		if got := r.URL.Query().Get("url"); got != "http://cp.cloudflare.com/generate_204" {
			t.Errorf("url query = %q", got)
		}
		if got := r.URL.Query().Get("timeout"); got != "5000" {
			t.Errorf("timeout query = %q", got)
		}
		_, _ = io.WriteString(w, `{"delay":42}`)
	}))
	defer server.Close()

	delay, err := NewMihomoAPI(server.URL, "").TestProxyDelayValue(name, "http://cp.cloudflare.com/generate_204", 5000)
	if err != nil {
		t.Fatalf("TestProxyDelayValue() error = %v", err)
	}
	if delay != 42 {
		t.Fatalf("delay = %d, want 42", delay)
	}
}

func TestGetProxyGroupsMergesProviderNodesForMihomo11928(t *testing.T) {
	useTestConfigDir(t)
	cfg := *GlobalConfig()
	cfg.MihomoRunningVersion = "1.19.28"
	SetGlobalConfig(cfg)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies":
			_, _ = io.WriteString(w, `{"proxies":{"Manual":{"name":"Manual","type":"Selector","all":["provider-node"]}}}`)
		case "/providers/proxies":
			_, _ = io.WriteString(w, `{"providers":{"provider1":{"proxies":[{"name":"provider-node","type":"Shadowsocks","history":[{"time":"2026-07-18T00:00:00Z","delay":88}]}]}}}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	groups, err := NewMihomoAPI(server.URL, "").GetProxyGroups()
	if err != nil {
		t.Fatalf("GetProxyGroups() error = %v", err)
	}
	if len(groups) != 1 || len(groups[0].Nodes) != 1 {
		t.Fatalf("groups = %#v, want one group with one provider node", groups)
	}
	if got, want := groups[0].Nodes[0].Type, "Shadowsocks"; got != want {
		t.Fatalf("provider node type = %q, want %q", got, want)
	}
	if got, want := groups[0].Nodes[0].Delay, 88; got != want {
		t.Fatalf("provider node delay = %d, want %d", got, want)
	}
}

func TestTestProxyDelayUsesProviderEndpointForMihomo11928(t *testing.T) {
	useTestConfigDir(t)
	cfg := *GlobalConfig()
	cfg.MihomoRunningVersion = "1.19.28"
	SetGlobalConfig(cfg)

	const node = "provider-node"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies/provider-node/delay":
			http.NotFound(w, r) // v1.19.28 no longer exposes provider nodes here.
		case "/providers/proxies":
			_, _ = io.WriteString(w, `{"providers":{"provider1":{"proxies":[{"name":"provider-node","type":"Shadowsocks"}]}}}`)
		case "/providers/proxies/provider1/provider-node/healthcheck":
			if got := r.URL.Query().Get("timeout"); got != "5000" {
				t.Errorf("timeout = %q, want 5000", got)
			}
			_, _ = io.WriteString(w, `{"delay":66}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	delay, err := NewMihomoAPI(server.URL, "").TestProxyDelayValue(node, "http://cp.cloudflare.com/generate_204", 5000)
	if err != nil {
		t.Fatalf("TestProxyDelayValue() error = %v", err)
	}
	if delay != 66 {
		t.Fatalf("delay = %d, want 66", delay)
	}
}

func TestUsesSeparatedProviderProxyAPI(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"", false},
		{"1.19.27", false},
		{"v1.19.28", true},
		{"1.20.0", true},
	}
	for _, tt := range tests {
		if got := usesSeparatedProviderProxyAPI(tt.version); got != tt.want {
			t.Errorf("usesSeparatedProviderProxyAPI(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}
