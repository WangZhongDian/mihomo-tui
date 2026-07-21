package mihomotui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resourceByKey(t *testing.T, key string) ExternalResourceInfo {
	t.Helper()
	for _, info := range CheckExternalResources() {
		if info.Key == key {
			return info
		}
	}
	t.Fatalf("resource %q not found", key)
	return ExternalResourceInfo{}
}

func TestSetExternalResourceURLOnlyChangesSelectedResource(t *testing.T) {
	useTestConfigDir(t)
	before := GlobalConfig().ExternalResources.GeoSite
	url := "https://example.invalid/custom-geoip.dat"
	if err := SetExternalResourceURL("geoip", url); err != nil {
		t.Fatalf("SetExternalResourceURL() error = %v", err)
	}
	cfg := GlobalConfig()
	if cfg.ExternalResources.GeoIP != url {
		t.Fatalf("GeoIP URL = %q, want %q", cfg.ExternalResources.GeoIP, url)
	}
	if cfg.ExternalResources.GeoSite != before {
		t.Fatalf("GeoSite URL unexpectedly changed: %q", cfg.ExternalResources.GeoSite)
	}
}

func TestUpdateExternalResourceIsAtomicOnFailure(t *testing.T) {
	useTestConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	if err := SetExternalResourceURL("geoip", server.URL); err != nil {
		t.Fatal(err)
	}
	path := externalResourcePath(mustExternalResourceSpec(t, "geoip"))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old-resource"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateExternalResource("geoip"); err == nil {
		t.Fatal("UpdateExternalResource() unexpectedly succeeded")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old-resource" {
		t.Fatalf("resource was overwritten after failed update: %q", body)
	}
}

func TestScanExternalResourceRejectsInvalidAndAcceptsManualFile(t *testing.T) {
	useTestConfigDir(t)
	spec := mustExternalResourceSpec(t, "geosite")
	path := externalResourcePath(spec)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanExternalResource(spec.key); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("empty manual resource error = %v, want non-empty validation error", err)
	}
	if err := os.WriteFile(path, []byte("manual-geosite"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanExternalResource(spec.key); err == nil || !strings.Contains(err.Error(), "权限过宽") {
		t.Fatalf("loose-permission resource error = %v, want permission validation error", err)
	}
	if err := os.WriteFile(path, []byte("manual-geosite"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := ScanExternalResource(spec.key)
	if err != nil {
		t.Fatalf("ScanExternalResource() error = %v", err)
	}
	if !info.Valid || !info.Exists || info.Size != int64(len("manual-geosite")) {
		t.Fatalf("unexpected scanned info: %+v", info)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0600 {
		t.Fatalf("manual resource mode = %04o, want 0600", got)
	}
}

func TestScanExternalResourceRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link permissions vary on Windows")
	}
	useTestConfigDir(t)
	spec := mustExternalResourceSpec(t, "geoip")
	path := externalResourcePath(spec)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(target, []byte("resource"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanExternalResource(spec.key); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("symlink resource error = %v, want symlink validation error", err)
	}
}

func TestImportManualMihomoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	useTestConfigDir(t)
	manualPath := ManualMihomoImportPath()
	if err := os.MkdirAll(filepath.Dir(manualPath), 0700); err != nil {
		t.Fatal(err)
	}
	const version = "9.8.7"
	script := "#!/bin/sh\necho 'Mihomo Meta v" + version + "'\n"
	if err := os.WriteFile(manualPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := ImportManualMihomoBinary()
	if err != nil {
		t.Fatalf("ImportManualMihomoBinary() error = %v", err)
	}
	if info.Version != version || !info.Manual || !info.Downloaded {
		t.Fatalf("unexpected manual import info: %+v", info)
	}
	if _, err := os.Lstat(manualPath); !os.IsNotExist(err) {
		t.Fatalf("manual source still exists or unexpected stat error: %v", err)
	}
	target := mihomoVersionBinaryPath(version)
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("import target missing: %v", err)
	}
	if got := st.Mode().Perm(); got != 0700 {
		t.Fatalf("imported binary mode = %04o, want 0700", got)
	}
	if err := DeleteMihomoVersion(version); err != nil {
		t.Fatalf("DeleteMihomoVersion() error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("deleted manual version still exists: %v", err)
	}
}

func mustExternalResourceSpec(t *testing.T, key string) externalResourceSpec {
	t.Helper()
	spec, err := findExternalResourceSpec(key)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
