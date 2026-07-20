package mihomotui

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSubscriptionMetadataHeadersVariants(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
	}{
		{"standard", "subscription-userinfo", "upload=1073741824; download=2147483648; total=4294967296; expire=1893456000"},
		{"amz", "x-amz-meta-subscription-userinfo", "upload=1073741824; download=2147483648; total=4294967296; expire=1893456000"},
		{"obs", "x-obs-meta-subscription-userinfo", "upload=1073741824; download=2147483648; total=4294967296; expire=1893456000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := make(http.Header)
			h.Set(tt.header, tt.value)
			h.Set("profile-update-interval", "24")
			got := parseSubscriptionMetadataHeaders(h)
			if !got.MetadataAvailable || got.UploadBytes != 1<<30 || got.DownloadBytes != 2<<30 || got.TotalBytes != 4<<30 {
				t.Fatalf("metadata not parsed correctly: %+v", got)
			}
			if got.ExpireAt == "" || got.ProfileUpdateInterval != 24 {
				t.Fatalf("expire/interval not parsed: %+v", got)
			}
		})
	}
}

func TestParseSubscriptionMetadataHeadersUnavailable(t *testing.T) {
	got := parseSubscriptionMetadataHeaders(http.Header{"X-Other": []string{"anything"}})
	if got.MetadataAvailable || got.MetadataStatus != "无法解析有效的订阅元数据" {
		t.Fatalf("unexpected missing metadata result: %+v", got)
	}
}

func TestSubscriptionUserAgentAndProxyStrategy(t *testing.T) {
	if defaultSubscriptionUserAgent == "" {
		t.Fatal("default subscription user-agent must be set")
	}
	if SubscriptionFetchDirect == SubscriptionFetchLocalMihomo || SubscriptionFetchSystem == SubscriptionFetchDirect {
		t.Fatal("proxy strategies must be distinct")
	}
}

func TestFetchSubscriptionUsesConfiguredUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.UserAgent(), "ClashMeta Mihomo-tui/1.0 clash"; got != want {
			http.Error(w, "unexpected user-agent", http.StatusForbidden)
			return
		}
		w.Header().Set("x-amz-meta-subscription-userinfo", "upload=1;download=2;total=3")
		_, _ = w.Write([]byte("ss://example-node"))
	}))
	defer server.Close()
	got, err := fetchSubscriptionWithOptions(server.URL, subscriptionFetchOptions{UserAgent: "ClashMeta Mihomo-tui/1.0 clash", Strategy: SubscriptionFetchDirect})
	if err != nil {
		t.Fatalf("fetchSubscriptionWithOptions() error = %v", err)
	}
	if !got.MetadataAvailable || got.TotalBytes != 3 {
		t.Fatalf("fetch result = %+v", got)
	}
}

func TestParseSubscriptionMetadataFromURIRemarks(t *testing.T) {
	content := []byte("vless://uuid@example.com:443?security=reality#%E5%89%A9%E4%BD%99%E6%B5%81%E9%87%8F%EF%BC%9A324.26%20GB\n" +
		"vless://uuid@example.com:443#%E5%A5%97%E9%A4%90%E5%88%B0%E6%9C%9F%EF%BC%9A%E9%95%BF%E6%9C%9F%E6%9C%89%E6%95%88")
	got := parseSubscriptionMetadataFromContent(content)
	want := (int64(32426) * 1024 * 1024 * 1024) / 100
	if !got.MetadataAvailable || got.RemainingBytes != want || got.ExpireAt != "长期有效" {
		t.Fatalf("URI remark metadata = %+v, want remaining=%d expiry=长期有效", got, want)
	}
}

func TestParseSubscriptionMetadataFromBase64URIRemarks(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("ss://example#Remaining%20traffic:%201.5%20GB"))
	got := parseSubscriptionMetadataFromContent([]byte(encoded))
	if !got.MetadataAvailable || got.RemainingBytes != (int64(15)*1024*1024*1024)/10 {
		t.Fatalf("base64 URI remark metadata = %+v", got)
	}
}

func TestContentImportParsesURIRemarkMetadata(t *testing.T) {
	useTestConfigDir(t)
	d := &Daemon{}
	content := []byte("vless://uuid@example.com:443#%E5%89%A9%E4%BD%99%E6%B5%81%E9%87%8F%EF%BC%9A324.26%20GB\n" +
		"vless://uuid@example.com:443#%E5%A5%97%E9%A4%90%E5%88%B0%E6%9C%9F%EF%BC%9A%E9%95%BF%E6%9C%9F%E6%9C%89%E6%95%88")
	if err := d.importSubscriptionContent("备注套餐", "pasted", SubscriptionSourceContent, content, subscriptionFetchResult{Content: content}, false); err != nil {
		t.Fatalf("importSubscriptionContent() error = %v", err)
	}
	got := GlobalConfig().Subscriptions[0]
	if !got.MetadataAvailable || got.RemainingBytes == 0 || got.ExpireAt != "长期有效" {
		t.Fatalf("imported metadata = %+v", got)
	}
}

func TestMergeHeaderExpiryWithURIRemarkQuota(t *testing.T) {
	headerOnlyExpiry := parseSubscriptionUserInfo("expire=1893456000")
	remark := parseSubscriptionMetadataFromContent([]byte("vless://uuid@example.com:443#%E5%89%A9%E4%BD%99%E6%B5%81%E9%87%8F%EF%BC%9A324.26%20GB"))
	got := mergeSubscriptionMetadata(headerOnlyExpiry, remark)
	if !got.MetadataAvailable || got.RemainingBytes == 0 || got.ExpireAt == "" {
		t.Fatalf("merged metadata = %+v", got)
	}
}

func TestParseSubscriptionUserInfoHumanReadableUnits(t *testing.T) {
	got := parseSubscriptionUserInfo("upload=75.2 GB; download=0 GB; total=400 GB")
	if !got.MetadataAvailable || got.UploadBytes != (int64(752)*1024*1024*1024)/10 || got.TotalBytes != 400*1024*1024*1024 {
		t.Fatalf("human readable header = %+v", got)
	}
}

func TestSubscriptionExpiryDoesNotIncludeNodeDescription(t *testing.T) {
	content := []byte("vless://uuid@example.com:443#%E5%A5%97%E9%A4%90%E5%88%B0%E6%9C%9F%EF%BC%9A%E9%95%BF%E6%9C%9F%E6%9C%89%E6%95%88%EF%BC%8C%E8%BF%87%E6%BB%A4%E6%8E%8915%E6%9D%A1%E7%BA%BF%E8%B7%AF%EF%BC%8C%E6%97%A5%E6%9C%AC-%E4%BC%98%E5%8C%96")
	got := parseSubscriptionMetadataFromContent(content)
	if got.ExpireAt != "长期有效" {
		t.Fatalf("expiry = %q, want only 长期有效", got.ExpireAt)
	}
	if normalized := normalizeSubscriptionExpiry("长期有效，过滤掉15条线路，日本-优化"); normalized != "长期有效" {
		t.Fatalf("normalized expiry = %q", normalized)
	}
}
