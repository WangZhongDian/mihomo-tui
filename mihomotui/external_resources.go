package mihomotui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	externalResourceGeoIP   = "geoip"
	externalResourceGeoSite = "geosite"
)

// ExternalResourceInfo describes one daemon-managed Geo data file. URL and Path
// are returned only to operator/root IPC callers; the read-only IPC policy never
// exposes this endpoint because URLs can contain credentials.
type ExternalResourceInfo struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Path      string    `json:"path"`
	Exists    bool      `json:"exists"`
	Valid     bool      `json:"valid"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// FormatSize formats a resource or version size for the TUI.
func FormatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(size)/float64(div), units[exp])
}

type externalResourceSpec struct {
	key      string
	name     string
	filename string
	url      func(ExternalResources) string
	setURL   func(*ExternalResources, string)
}

var externalResourceOperationState = struct {
	sync.RWMutex
	errors map[string]string
}{errors: make(map[string]string)}

func externalResourceSpecs() []externalResourceSpec {
	return []externalResourceSpec{
		{key: externalResourceGeoIP, name: "GeoIP", filename: "geoip.dat", url: func(c ExternalResources) string { return c.GeoIP }, setURL: func(c *ExternalResources, u string) { c.GeoIP = u }},
		{key: externalResourceGeoSite, name: "GeoSite", filename: "geosite.dat", url: func(c ExternalResources) string { return c.GeoSite }, setURL: func(c *ExternalResources, u string) { c.GeoSite = u }},
	}
}

func findExternalResourceSpec(key string) (externalResourceSpec, error) {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, spec := range externalResourceSpecs() {
		if normalized == spec.key || normalized == strings.ToLower(spec.name) || normalized == spec.filename {
			return spec, nil
		}
	}
	return externalResourceSpec{}, fmt.Errorf("未知外部资源: %q（仅支持 GeoIP 或 GeoSite）", key)
}

func externalResourcePath(spec externalResourceSpec) string {
	return filepath.Join(GetConfigDir(), "mihomo", spec.filename)
}

func setExternalResourceError(key string, err error) {
	externalResourceOperationState.Lock()
	defer externalResourceOperationState.Unlock()
	if err == nil {
		delete(externalResourceOperationState.errors, key)
		return
	}
	externalResourceOperationState.errors[key] = RedactURLInText(err.Error())
}

func externalResourceLastError(key string) string {
	externalResourceOperationState.RLock()
	defer externalResourceOperationState.RUnlock()
	return externalResourceOperationState.errors[key]
}

// validateManagedExternalFile accepts only a regular, non-empty file. Group or
// world writable files are rejected; readable files are tightened to 0600 once
// accepted so root daemons do not keep loose permissions in their private dir.
func validateManagedExternalFile(path string, tighten bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("手动文件不能是符号链接")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("手动文件必须是普通文件")
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("手动文件不能为空")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("手动文件权限过宽（禁止组/其他用户写入）")
	}
	if tighten {
		if err := os.Chmod(path, 0600); err != nil {
			return nil, fmt.Errorf("收紧文件权限失败: %w", err)
		}
		return os.Stat(path)
	}
	return info, nil
}

// CheckExternalResources returns both persistent resource configuration and
// safe local-file status. It only inspects project-owned fixed paths.
func CheckExternalResources() []ExternalResourceInfo {
	cfg := GlobalConfig()
	infos := make([]ExternalResourceInfo, 0, len(externalResourceSpecs()))
	for _, spec := range externalResourceSpecs() {
		info := ExternalResourceInfo{
			Key:       spec.key,
			Name:      spec.name,
			URL:       spec.url(cfg.ExternalResources),
			Path:      externalResourcePath(spec),
			LastError: externalResourceLastError(spec.key),
		}
		if fileInfo, err := validateManagedExternalFile(info.Path, false); err == nil {
			info.Exists, info.Valid, info.Size, info.ModTime = true, true, fileInfo.Size(), fileInfo.ModTime()
		} else if !os.IsNotExist(err) {
			info.Exists = true
			if info.LastError == "" {
				info.LastError = err.Error()
			}
		}
		infos = append(infos, info)
	}
	return infos
}

func validateExternalResourceURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("外部资源下载地址必须是合法 http/https URL")
	}
	return rawURL, nil
}

// SetExternalResourceURL persists exactly one resource URL. The client cannot
// choose a destination path; downloads always use the daemon private directory.
func SetExternalResourceURL(key, rawURL string) error {
	spec, err := findExternalResourceSpec(key)
	if err != nil {
		return err
	}
	rawURL, err = validateExternalResourceURL(rawURL)
	if err != nil {
		return fmt.Errorf("%s URL 无效: %w", spec.name, err)
	}
	_, err = UpdateGlobalConfig(func(c *Config) error {
		spec.setURL(&c.ExternalResources, rawURL)
		return nil
	})
	return err
}

func ensureExternalResourceDir() (string, error) {
	dir := filepath.Join(GetConfigDir(), "mihomo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("创建工作目录失败: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", fmt.Errorf("收紧工作目录权限失败: %w", err)
	}
	return dir, nil
}

// UpdateExternalResource force-downloads one selected resource atomically.
// A failed request never replaces a previously valid resource file.
func UpdateExternalResource(key string) (ExternalResourceInfo, error) {
	spec, err := findExternalResourceSpec(key)
	if err != nil {
		return ExternalResourceInfo{}, err
	}
	cfg := GlobalConfig()
	rawURL := spec.url(cfg.ExternalResources)
	if _, err := validateExternalResourceURL(rawURL); err != nil {
		return ExternalResourceInfo{}, fmt.Errorf("%s URL 无效: %w", spec.name, err)
	}
	if _, err := ensureExternalResourceDir(); err != nil {
		return ExternalResourceInfo{}, err
	}
	dst := externalResourcePath(spec)
	Infof("开始下载外部资源: %s -> %s", spec.name, RedactURL(rawURL))
	if err := downloadFile(rawURL, dst, nil); err != nil {
		err = fmt.Errorf("下载 %s 失败: %w", spec.name, err)
		setExternalResourceError(spec.key, err)
		return ExternalResourceInfo{}, err
	}
	if _, err := validateManagedExternalFile(dst, true); err != nil {
		err = fmt.Errorf("验证 %s 失败: %w", spec.name, err)
		setExternalResourceError(spec.key, err)
		return ExternalResourceInfo{}, err
	}
	setExternalResourceError(spec.key, nil)
	for _, info := range CheckExternalResources() {
		if info.Key == spec.key {
			return info, nil
		}
	}
	return ExternalResourceInfo{}, fmt.Errorf("读取 %s 更新后的状态失败", spec.name)
}

// ScanExternalResource accepts only an already placed file at the fixed daemon
// target path. It never receives or follows a client supplied path.
func ScanExternalResource(key string) (ExternalResourceInfo, error) {
	spec, err := findExternalResourceSpec(key)
	if err != nil {
		return ExternalResourceInfo{}, err
	}
	if _, err := validateManagedExternalFile(externalResourcePath(spec), true); err != nil {
		err = fmt.Errorf("扫描 %s 手动文件失败: %w", spec.name, err)
		setExternalResourceError(spec.key, err)
		return ExternalResourceInfo{}, err
	}
	setExternalResourceError(spec.key, nil)
	for _, info := range CheckExternalResources() {
		if info.Key == spec.key {
			return info, nil
		}
	}
	return ExternalResourceInfo{}, fmt.Errorf("读取 %s 扫描结果失败", spec.name)
}

// DownloadExternalResources is the legacy bulk entry point. It retains old IPC
// compatibility while using the same per-resource safe update implementation.
func DownloadExternalResources() error {
	var failures []string
	for _, spec := range externalResourceSpecs() {
		if _, err := UpdateExternalResource(spec.key); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return nil
}
