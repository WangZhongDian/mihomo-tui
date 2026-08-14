package mihomotui

import (
	"net"
	"strings"
	"testing"
)

func TestParseAndFormatExcludeAddresses(t *testing.T) {
	text := "192.168.0.0/16\n\n10.0.0.0/8, netbird.example.com\n10.0.0.0/8"
	got := ParseExcludeAddresses(text)
	want := []string{"192.168.0.0/16", "10.0.0.0/8", "netbird.example.com"}
	if len(got) != len(want) {
		t.Fatalf("ParseExcludeAddresses() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseExcludeAddresses()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if formatted := FormatExcludeAddresses(got); formatted != strings.Join(want, "\n") {
		t.Fatalf("FormatExcludeAddresses() = %q, want %q", formatted, strings.Join(want, "\n"))
	}
}

func TestIsValidExcludeAddress(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.1.1.1", true},
		{"192.168.0.0/16", true},
		{"::1", true},
		{"fc00::/7", true},
		{"netbird.example.com", true},
		{"localhost", true},
		{"", false},
		{"not a host", false},
		{"1.2.3", false},
		{"-bad.example.com", false},
	}
	for _, tc := range cases {
		if got := IsValidExcludeAddress(tc.in); got != tc.want {
			t.Fatalf("IsValidExcludeAddress(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildRouteExcludeAddressesMergesWhenNonEmpty(t *testing.T) {
	cfg := defaultConfig()
	base := cfg.buildRouteExcludeAddresses()
	if len(base) == 0 {
		t.Fatal("default route excludes should not be empty")
	}

	// 空列表时不合并用户地址
	cfg.ExcludeAddresses = nil
	if got := cfg.buildRouteExcludeAddresses(); len(got) != len(base) {
		t.Fatalf("empty excludes should keep default only: got %d want %d", len(got), len(base))
	}

	// 列表不为空即开启，无需 GroupNetVPN 开关
	cfg.ExcludeAddresses = []string{"203.0.113.10", "198.51.100.0/24", "203.0.113.10"}
	got := cfg.buildRouteExcludeAddresses()
	wantExtra := []string{"203.0.113.10/32", "198.51.100.0/24"}
	for _, extra := range wantExtra {
		found := false
		for _, item := range got {
			if item == extra {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("merge missing %q in %#v", extra, got)
		}
	}
	// 默认项仍保留
	for _, d := range defaultRouteExcludeAddresses() {
		found := false
		for _, item := range got {
			if item == d {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("default exclude %q missing after merge: %#v", d, got)
		}
	}
}

func TestExpandExcludeAddressIPAndDomain(t *testing.T) {
	if got := expandExcludeAddress("8.8.8.8"); len(got) != 1 || got[0] != "8.8.8.8/32" {
		t.Fatalf("expand IP = %#v, want [8.8.8.8/32]", got)
	}
	if got := expandExcludeAddress("2001:db8::1"); len(got) != 1 || got[0] != "2001:db8::1/128" {
		t.Fatalf("expand IPv6 = %#v, want [2001:db8::1/128]", got)
	}
	// 使用 loopback 主机名，保证解析稳定
	ips, err := net.LookupIP("localhost")
	if err != nil || len(ips) == 0 {
		t.Skip("localhost resolve unavailable")
	}
	got := expandExcludeAddress("localhost")
	if len(got) == 0 {
		t.Fatalf("expand localhost returned empty")
	}
}

func TestValidateExcludeAddresses(t *testing.T) {
	cfg := defaultConfig()
	cfg.ExcludeAddresses = []string{"1.1.1.1", "bad host"}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "exclude_addresses") {
		t.Fatalf("Validate() = %v, want exclude_addresses error", err)
	}
	cfg.ExcludeAddresses = []string{"1.1.1.1", "10.0.0.0/8", "example.com"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestCloneCopiesExcludeAddresses(t *testing.T) {
	cfg := defaultConfig()
	cfg.ExcludeAddresses = []string{"10.0.0.0/8"}
	cp := cfg.Clone()
	cp.ExcludeAddresses[0] = "changed"
	if cfg.ExcludeAddresses[0] != "10.0.0.0/8" {
		t.Fatalf("Clone did not deep-copy ExcludeAddresses")
	}
}

func TestBuildFakeIPFilterDefaults(t *testing.T) {
	cfg := defaultConfig()
	got := cfg.buildFakeIPFilter()
	want := []string{"*.lan", "localhost.ptlogin2.qq.com"}
	if len(got) != len(want) {
		t.Fatalf("buildFakeIPFilter() = %#v, want %#v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("buildFakeIPFilter()[%d] = %q, want %q", i, got[i], v)
		}
	}
	// 默认不应包含会全局关闭 fake-ip 的 "*"
	for _, v := range got {
		if v == "*" {
			t.Fatalf("buildFakeIPFilter() must not contain bare *")
		}
	}
}

func TestBuildFakeIPFilterMergesCustom(t *testing.T) {
	cfg := defaultConfig()
	cfg.FakeIPFilter = []string{"+.youmetme.wang", "*.lan", "  extra.example.com  "}
	got := cfg.buildFakeIPFilter()
	// 期望：默认两条在前，自定义去重追加，空白被裁剪，重复 *.lan 不再出现
	want := []string{"*.lan", "localhost.ptlogin2.qq.com", "+.youmetme.wang", "extra.example.com"}
	if len(got) != len(want) {
		t.Fatalf("buildFakeIPFilter() = %#v, want %#v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("buildFakeIPFilter()[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestIsValidFakeIPFilterEntry(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"*.lan", true},
		{"+.youmetme.wang", true},
		{"netbird.example.com", true},
		{"localhost.ptlogin2.qq.com", true},
		{"localhost", true},
		{"*", false},  // 裸 * 会全局关闭 fake-ip，必须拒绝
		{"", false},   // 空
		{"*.", false}, // 前缀后无主机名
		{"-bad.com", false},
		{"1.2.3", false}, // 纯数字多段伪域名
	}
	for _, c := range cases {
		if got := IsValidFakeIPFilterEntry(c.in); got != c.want {
			t.Fatalf("IsValidFakeIPFilterEntry(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidateFakeIPFilter(t *testing.T) {
	cfg := defaultConfig()
	cfg.FakeIPFilter = []string{"valid.com", "*", ""}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate() expected error for bad fake_ip_filter entries, got nil")
	}
	cfg.FakeIPFilter = []string{"+.a.com", "b.org"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
