package mihomotui

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const mihomoVersionListLimit = 30
const mihomoReleaseResponseLimit = 16 << 20
const mihomoVersionsDirName = "versions"

// MihomoVersionInfo describes a release known to the daemon. AssetURL is only
// an official release asset URL or a generated mirror URL and contains no user credential.
type MihomoVersionInfo struct {
	Version     string `yaml:"version" json:"version"`
	PublishedAt string `yaml:"published_at,omitempty" json:"published_at,omitempty"`
	Prerelease  bool   `yaml:"prerelease" json:"prerelease"`
	Downloaded  bool   `yaml:"downloaded" json:"downloaded"`
	Active      bool   `yaml:"active" json:"active"`
	Path        string `yaml:"path,omitempty" json:"path,omitempty"`
	Size        int64  `yaml:"size,omitempty" json:"size,omitempty"`
	Source      string `yaml:"source,omitempty" json:"source,omitempty"`
	AssetURL    string `yaml:"asset_url,omitempty" json:"asset_url,omitempty"`
	AssetName   string `yaml:"asset_name,omitempty" json:"asset_name,omitempty"`
}

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	PublishedAt string               `json:"published_at"`
	Prerelease  bool                 `json:"prerelease"`
	Draft       bool                 `json:"draft"`
	Assets      []githubReleaseAsset `json:"assets"`
}
type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

var mihomoVersionHTTPClient = &http.Client{Timeout: httpTimeout}
var mihomoVersionSources = []struct{ name, url string }{
	{"GitHub", "https://api.github.com/repos/MetaCubeX/mihomo/releases?per_page=30"},
	{"GitHub 备用源", "https://ghproxy.net/https://api.github.com/repos/MetaCubeX/mihomo/releases?per_page=30"},
}
var mihomoVersionOpMu sync.Mutex

func mihomoVersionBinaryPath(version string) string {
	name := mihomoBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(GetConfigDir(), "bin", mihomoVersionsDirName, "v"+strings.TrimPrefix(version, "v"), name)
}

func normalizeMihomoVersion(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }

func fetchMihomoReleaseCatalog() ([]MihomoVersionInfo, string, error) {
	var failures []string
	for _, source := range mihomoVersionSources {
		// Some network proxies truncate large GitHub JSON responses. Retry each
		// source once and reject a response that reaches the explicit size limit
		// instead of passing an incomplete document to the JSON decoder.
		for attempt := 1; attempt <= 2; attempt++ {
			data, err := requestMihomoReleaseList(source.url)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s（第 %d 次）: %v", source.name, attempt, err))
				continue
			}
			releases, err := parseMihomoReleases(data, source.name)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s（第 %d 次）: %v", source.name, attempt, err))
				continue
			}
			return releases, source.name, nil
		}
	}
	return nil, "", fmt.Errorf("检查 mihomo 版本失败（%s）", strings.Join(failures, "；"))
}

func requestMihomoReleaseList(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mihomo-tui")
	// Avoid intermediary/proxy bugs around transparently recompressed bodies.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := mihomoVersionHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %s", RedactURLInText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mihomoReleaseResponseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("响应为空")
	}
	if len(data) > mihomoReleaseResponseLimit {
		return nil, fmt.Errorf("响应过大（超过 %d MiB）", mihomoReleaseResponseLimit>>20)
	}
	return data, nil
}

func parseMihomoReleases(data []byte, source string) ([]MihomoVersionInfo, error) {
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("版本列表格式无效: %w", err)
	}
	result := make([]MihomoVersionInfo, 0, len(releases))
	seen := map[string]bool{}
	for _, r := range releases {
		v := normalizeMihomoVersion(r.TagName)
		if r.Draft || v == "" || seen[v] {
			continue
		}
		asset, ok := selectMihomoReleaseAsset(r.Assets, runtime.GOOS, runtime.GOARCH)
		if !ok {
			continue
		}
		seen[v] = true
		result = append(result, MihomoVersionInfo{Version: v, PublishedAt: r.PublishedAt, Prerelease: r.Prerelease, Source: source, AssetURL: asset.BrowserDownloadURL, AssetName: asset.Name, Size: asset.Size})
		if len(result) == mihomoVersionListLimit {
			break
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("未找到适用于 %s/%s 的发布资产", runtime.GOOS, runtime.GOARCH)
	}
	return result, nil
}

func selectMihomoReleaseAsset(assets []githubReleaseAsset, goos, goarch string) (githubReleaseAsset, bool) {
	arch := goarch
	if arch == "386" {
		arch = "386"
	}
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, strings.ToLower(goos+"-"+arch)) && (strings.HasSuffix(n, ".gz") || strings.HasSuffix(n, ".zip")) && !strings.Contains(n, "compatible") {
			return a, true
		}
	}
	return githubReleaseAsset{}, false
}

// RefreshMihomoVersionCatalog updates the persisted release cache. If refresh fails,
// existing cache is retained and the failure diagnostic is persisted.
func RefreshMihomoVersionCatalog() ([]MihomoVersionInfo, error) {
	infos, source, err := fetchMihomoReleaseCatalog()
	now := time.Now().Format(time.RFC3339)
	if err != nil {
		_, _ = UpdateGlobalConfig(func(c *Config) error {
			c.MihomoVersionsLastError = err.Error()
			c.MihomoVersionsCheckedAt = now
			return nil
		})
		return MihomoVersionList(), err
	}
	_, err = UpdateGlobalConfig(func(c *Config) error {
		c.MihomoVersions = infos
		c.MihomoVersionsSource = source
		c.MihomoVersionsCheckedAt = now
		c.MihomoVersionsLastError = ""
		return nil
	})
	if err != nil {
		return nil, err
	}
	return MihomoVersionList(), nil
}

// MigrateLegacyMihomoBinary imports the old single-binary installation into
// the version repository once. System/PATH binaries are never copied or deleted.
func MigrateLegacyMihomoBinary() {
	cfg := GlobalConfig()
	if cfg.MihomoActiveVersion != "" || cfg.MihomoBinaryPath == "" {
		return
	}
	legacyPath := cfg.MihomoBinaryPath
	managedLegacyPath := filepath.Join(GetConfigDir(), "bin", mihomoBinaryName)
	if runtime.GOOS == "windows" {
		managedLegacyPath += ".exe"
	}
	if filepath.Clean(legacyPath) != filepath.Clean(managedLegacyPath) {
		return
	}
	out, err := exec.Command(legacyPath, "-v").CombinedOutput()
	if err != nil {
		Warnf("无法迁移旧 mihomo 内核: %v", err)
		return
	}
	version := parseVersion(string(out))
	if version == "" {
		Warnf("无法识别旧 mihomo 内核版本")
		return
	}
	target := mihomoVersionBinaryPath(version)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		Warnf("创建版本目录失败: %v", err)
		return
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		data, readErr := os.ReadFile(legacyPath)
		if readErr != nil {
			Warnf("读取旧 mihomo 内核失败: %v", readErr)
			return
		}
		tmp := target + ".migrate.tmp"
		if writeErr := os.WriteFile(tmp, data, 0700); writeErr != nil {
			Warnf("写入迁移内核失败: %v", writeErr)
			return
		}
		if renameErr := os.Rename(tmp, target); renameErr != nil {
			_ = os.Remove(tmp)
			Warnf("迁移内核失败: %v", renameErr)
			return
		}
	}
	if _, err := UpdateGlobalConfig(func(c *Config) error { c.MihomoActiveVersion = version; c.MihomoBinaryPath = target; return nil }); err != nil {
		Warnf("保存迁移后的内核版本失败: %v", err)
	}
}

// MihomoVersionList returns cached versions augmented with local installation state.
func MihomoVersionList() []MihomoVersionInfo {
	cfg := GlobalConfig()
	list := append([]MihomoVersionInfo(nil), cfg.MihomoVersions...)
	byVersion := map[string]int{}
	for i := range list {
		byVersion[list[i].Version] = i
	}
	entries, _ := os.ReadDir(filepath.Join(GetConfigDir(), "bin", mihomoVersionsDirName))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		v := normalizeMihomoVersion(entry.Name())
		if _, ok := byVersion[v]; !ok {
			list = append(list, MihomoVersionInfo{Version: v, Source: "本地缓存"})
			byVersion[v] = len(list) - 1
		}
	}
	for i := range list {
		p := mihomoVersionBinaryPath(list[i].Version)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			list[i].Downloaded = true
			list[i].Path = p
			list[i].Size = st.Size()
		}
		list[i].Active = normalizeMihomoVersion(cfg.MihomoActiveVersion) == list[i].Version
	}
	sort.SliceStable(list, func(i, j int) bool { return compareMihomoVersions(list[i].Version, list[j].Version) > 0 })
	return list
}

// compareMihomoVersions compares semantic versions numerically. It deliberately
// does not use lexical ordering: 1.19.28 must appear before 1.19.9.
func compareMihomoVersions(left, right string) int {
	lBase, lPre, _ := strings.Cut(normalizeMihomoVersion(left), "-")
	rBase, rPre, _ := strings.Cut(normalizeMihomoVersion(right), "-")
	lParts, rParts := strings.Split(lBase, "."), strings.Split(rBase, ".")
	for i := 0; i < 3; i++ {
		lv, lok := versionNumberAt(lParts, i)
		rv, rok := versionNumberAt(rParts, i)
		if !lok || !rok {
			// Release tags should be semantic versions; retain deterministic
			// behavior for an unexpected tag instead of failing the whole list.
			if left > right {
				return 1
			}
			if left < right {
				return -1
			}
			return 0
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	// A stable release is newer than its prerelease of the same base version.
	if lPre == "" && rPre != "" {
		return 1
	}
	if lPre != "" && rPre == "" {
		return -1
	}
	if lPre > rPre {
		return 1
	}
	if lPre < rPre {
		return -1
	}
	return 0
}
func versionNumberAt(parts []string, index int) (int, bool) {
	if index >= len(parts) {
		return 0, true
	}
	value, err := strconv.Atoi(parts[index])
	return value, err == nil && value >= 0
}

func MihomoVersionCacheStatus() (checkedAt, source, lastError string) {
	c := GlobalConfig()
	return c.MihomoVersionsCheckedAt, c.MihomoVersionsSource, c.MihomoVersionsLastError
}
func FindMihomoVersionInfo(version string) (MihomoVersionInfo, bool) {
	v := normalizeMihomoVersion(version)
	for _, info := range MihomoVersionList() {
		if info.Version == v {
			return info, true
		}
	}
	return MihomoVersionInfo{}, false
}

func downloadMihomoVersion(info MihomoVersionInfo, progress func(DownloadProgress)) (string, error) {
	if info.AssetURL == "" {
		return "", fmt.Errorf("版本 v%s 没有适用于当前平台的下载资产，请先刷新版本列表", info.Version)
	}
	target := mihomoVersionBinaryPath(info.Version)
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		if progress != nil {
			progress(DownloadProgress{Percent: 100, Status: "done"})
		}
		return target, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	_ = os.Chmod(filepath.Dir(target), 0700)
	temp, err := os.CreateTemp("", "mihomo-release-*")
	if err != nil {
		return "", err
	}
	archive := temp.Name()
	if err = temp.Close(); err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err = downloadMihomoAsset(info.AssetURL, archive, progress); err != nil {
		return "", err
	}
	if progress != nil {
		progress(DownloadProgress{Percent: 100, Status: "extracting"})
	}
	tmp := target + ".tmp"
	_ = os.Remove(tmp)
	if strings.HasSuffix(strings.ToLower(info.AssetName), ".zip") {
		err = extractMihomoZip(archive, tmp)
	} else {
		err = extractGzip(archive, tmp)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("解压 v%s 失败: %w", info.Version, err)
	}
	if runtime.GOOS != "windows" {
		if err = os.Chmod(tmp, 0700); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
	}
	if _, err = exec.Command(tmp, "-v").CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("下载的内核无法执行: %w", err)
	}
	if err = os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if progress != nil {
		progress(DownloadProgress{Percent: 100, Status: "done"})
	}
	return target, nil
}
func downloadMihomoAsset(rawURL, dst string, progress func(DownloadProgress)) error {
	if err := downloadFileWithProgress(rawURL, dst, progress); err == nil {
		return nil
	} else if strings.Contains(rawURL, "github.com/") {
		mirrorURL := "https://ghproxy.net/" + rawURL
		Warnf("官方 mihomo 下载失败，尝试备用源: %v", err)
		if mirrorErr := downloadFileWithProgress(mirrorURL, dst, progress); mirrorErr == nil {
			return nil
		} else {
			return fmt.Errorf("官方下载失败: %v；备用源下载失败: %v", err, mirrorErr)
		}
	} else {
		return err
	}
}
func extractMihomoZip(src, dst string) error {
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		n := strings.ToLower(filepath.Base(f.Name))
		if n == "mihomo" || n == "mihomo.exe" {
			r, e := f.Open()
			if e != nil {
				return e
			}
			defer r.Close()
			out, e := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if e != nil {
				return e
			}
			_, e = io.Copy(out, r)
			ce := out.Close()
			if e != nil {
				return e
			}
			return ce
		}
	}
	return fmt.Errorf("压缩包中未找到 mihomo 可执行文件")
}

func DownloadMihomoVersionWithProgress(version string, progress func(DownloadProgress)) (string, error) {
	mihomoVersionOpMu.Lock()
	defer mihomoVersionOpMu.Unlock()
	info, ok := FindMihomoVersionInfo(version)
	if !ok {
		return "", fmt.Errorf("版本 v%s 不在本地版本列表中，请先检查更新", normalizeMihomoVersion(version))
	}
	return downloadMihomoVersion(info, progress)
}
func DownloadMihomoVersion(version string, progress func(int, string)) (string, error) {
	return DownloadMihomoVersionWithProgress(version, func(p DownloadProgress) {
		if progress != nil {
			progress(p.Percent, p.Status)
		}
	})
}
func ActivateMihomoVersion(version string) error {
	mihomoVersionOpMu.Lock()
	defer mihomoVersionOpMu.Unlock()
	v := normalizeMihomoVersion(version)
	p := mihomoVersionBinaryPath(v)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("版本 v%s 尚未下载", v)
	}
	out, err := exec.Command(p, "-v").CombinedOutput()
	if err != nil {
		return fmt.Errorf("验证版本 v%s 失败: %w", v, err)
	}
	if got := parseVersion(string(out)); got == "" {
		return fmt.Errorf("无法验证版本 v%s", v)
	}
	_, err = UpdateGlobalConfig(func(c *Config) error { c.MihomoActiveVersion = v; c.MihomoBinaryPath = p; return nil })
	return err
}
func DeleteMihomoVersion(version string) error {
	mihomoVersionOpMu.Lock()
	defer mihomoVersionOpMu.Unlock()
	v := normalizeMihomoVersion(version)
	if normalizeMihomoVersion(GlobalConfig().MihomoActiveVersion) == v {
		return fmt.Errorf("当前正在使用 v%s，切换到其他已下载版本后才能删除", v)
	}
	p := mihomoVersionBinaryPath(v)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("版本 v%s 未下载", v)
		}
		return err
	}
	return os.RemoveAll(filepath.Dir(p))
}

func latestStableMihomoVersion() (MihomoVersionInfo, error) {
	for _, i := range MihomoVersionList() {
		if !i.Prerelease {
			return i, nil
		}
	}
	return MihomoVersionInfo{}, fmt.Errorf("版本列表中没有稳定版")
}

// DownloadMihomo retains compatibility for old callers: it downloads then activates the chosen version.
func DownloadMihomo(version string, onProgress func(int, string)) (string, error) {
	if _, ok := FindMihomoVersionInfo(version); !ok {
		if _, err := RefreshMihomoVersionCatalog(); err != nil {
			return "", err
		}
	}
	p, err := DownloadMihomoVersion(version, onProgress)
	if err != nil {
		return "", err
	}
	if err = ActivateMihomoVersion(version); err != nil {
		return "", err
	}
	return p, nil
}
