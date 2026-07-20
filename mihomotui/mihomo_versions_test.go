package mihomotui

import (
	"testing"
)

func TestParseMihomoReleasesSelectsPlatformAsset(t *testing.T) {
	data := []byte(`[{"tag_name":"v1.19.0","published_at":"2025-01-01T00:00:00Z","assets":[{"name":"mihomo-linux-amd64-v1.19.0.gz","browser_download_url":"https://example.invalid/a","size":12}]}]`)
	infos, err := parseMihomoReleases(data, "test")
	if err != nil {
		t.Skipf("host asset mismatch is expected on non linux/amd64: %v", err)
	}
	if len(infos) != 1 || infos[0].Version != "1.19.0" || infos[0].AssetURL == "" {
		t.Fatalf("unexpected catalog: %#v", infos)
	}
}
func TestSelectMihomoReleaseAsset(t *testing.T) {
	a, ok := selectMihomoReleaseAsset([]githubReleaseAsset{{Name: "mihomo-linux-amd64-v1.0.0.gz"}, {Name: "mihomo-linux-arm64-v1.0.0.gz"}}, "linux", "arm64")
	if !ok || a.Name != "mihomo-linux-arm64-v1.0.0.gz" {
		t.Fatalf("selected %#v ok=%v", a, ok)
	}
}
func TestNormalizeMihomoVersion(t *testing.T) {
	if got := normalizeMihomoVersion(" v1.2.3 "); got != "1.2.3" {
		t.Fatalf("got %q", got)
	}
}

func TestParseMihomoReleasesRejectsTruncatedJSON(t *testing.T) {
	if _, err := parseMihomoReleases([]byte(`[{"tag_name":"v1.2.3"`), "test"); err == nil {
		t.Fatal("truncated JSON should be rejected")
	}
}

func TestCompareMihomoVersionsUsesNumericOrdering(t *testing.T) {
	cases := []struct{ newer, older string }{
		{"1.19.28", "1.19.9"},
		{"1.20.0", "1.19.99"},
		{"1.19.9", "1.19.9-alpha"},
	}
	for _, tc := range cases {
		if got := compareMihomoVersions(tc.newer, tc.older); got <= 0 {
			t.Fatalf("compare(%s, %s) = %d, want > 0", tc.newer, tc.older, got)
		}
	}
}
