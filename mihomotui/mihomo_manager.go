package mihomotui

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	mihomoRepo       = "MetaCubeX/mihomo"
	githubAPIURL     = "https://api.github.com/repos/" + mihomoRepo + "/releases/latest"
	githubReleaseURL = "https://github.com/" + mihomoRepo + "/releases/download"
	mihomoBinaryName = "mihomo"
	httpTimeout      = 30 * time.Second
)

// releaseInfo GitHub Release API 响应结构
type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// GetMihomoLastVersion 从 GitHub API 获取最新版本号（如 v1.19.0）
func GetMihomoLastVersion() (string, error) {
	Infof("请求 GitHub API: %s", githubAPIURL)
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest("GET", githubAPIURL, nil)
	if err != nil {
		Errorf("创建请求失败: %v", err)
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "mihomo-tui")

	resp, err := client.Do(req)
	if err != nil {
		Errorf("请求 GitHub API 失败: %v", err)
		return "", fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		Errorf("GitHub API 返回非 200: %d, %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("GitHub API 返回非 200: %d, %s", resp.StatusCode, string(body))
	}

	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		Errorf("解析 GitHub API 响应失败: %v", err)
		return "", fmt.Errorf("解析 GitHub API 响应失败: %w", err)
	}

	version := strings.TrimPrefix(info.TagName, "v")
	Infof("获取到最新版本: %s", version)
	return version, nil
}

// GetCurrentMihomoVersion 获取当前安装的 mihomo 版本
// 优先从配置目录查找二进制，其次从 PATH 查找
func GetCurrentMihomoVersion() (string, error) {
	binaryPath := findMihomoBinary()
	if binaryPath == "" {
		Errorf("未找到 mihomo 可执行文件")
		return "", fmt.Errorf("未找到 mihomo 可执行文件")
	}
	Infof("找到 mihomo 二进制文件: %s", binaryPath)

	cmd := exec.Command(binaryPath, "-v")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 尝试 mihomo version 子命令
		cmd = exec.Command(binaryPath, "version")
		output, err = cmd.CombinedOutput()
		if err != nil {
			Errorf("执行 mihomo 版本命令失败: %v", err)
			return "", fmt.Errorf("执行 mihomo 版本命令失败: %w", err)
		}
	}
	Debugf("mihomo 版本命令输出: %s", string(output))

	version := parseVersion(string(output))
	if version == "" {
		Errorf("无法从输出中解析版本号: %s", string(output))
		return "", fmt.Errorf("无法从输出中解析版本号: %s", string(output))
	}
	Infof("当前 mihomo 版本: %s", version)
	return version, nil
}

// FindMihomoBinary 查找 mihomo 可执行文件路径（导出）
func FindMihomoBinary() string {
	return findMihomoBinary()
}

// findMihomoBinary 查找 mihomo 可执行文件路径
// 优先级：1. 配置指定路径 > 2. 配置目录 bin/ > 3. PATH > 4. 系统常用目录兜底
func findMihomoBinary() string {
	// 1. 优先使用配置中指定的绝对路径
	cfg := GlobalConfig()
	if cfg.MihomoBinaryPath != "" {
		if _, err := os.Stat(cfg.MihomoBinaryPath); err == nil {
			Infof("使用配置指定路径找到 mihomo: %s", cfg.MihomoBinaryPath)
			return cfg.MihomoBinaryPath
		}
		Warnf("配置指定的 mihomo 路径不存在: %s", cfg.MihomoBinaryPath)
	}

	// 2. 查找配置目录下的 bin/ 子目录
	configDir := GetConfigDir()
	if configDir != "" {
		localPath := filepath.Join(configDir, "bin", mihomoBinaryName)
		if runtime.GOOS == "windows" {
			localPath += ".exe"
		}
		Debugf("查找 mihomo 二进制文件: %s", localPath)
		if _, err := os.Stat(localPath); err == nil {
			Infof("在配置目录找到 mihomo: %s", localPath)
			return localPath
		}
	}

	// 3. 从 PATH 查找
	Debugf("从 PATH 查找 mihomo: %s", mihomoBinaryName)
	if path, err := exec.LookPath(mihomoBinaryName); err == nil {
		Infof("在 PATH 中找到 mihomo: %s", path)
		return path
	}

	// 4. 兜底：检查常用系统目录（适用于 systemd 等 PATH 受限场景）
	if runtime.GOOS != "windows" {
		fallbackDirs := []string{"/usr/local/bin", "/usr/bin", "/opt/bin"}
		for _, dir := range fallbackDirs {
			p := filepath.Join(dir, mihomoBinaryName)
			if _, err := os.Stat(p); err == nil {
				Infof("在系统目录找到 mihomo: %s", p)
				return p
			}
		}
	}

	Warnf("未找到 mihomo 二进制文件")
	return ""
}

// versionRegex 匹配版本号，支持 v1.19.0、version 1.19.0、1.19.0-alpha 等格式
var versionRegex = regexp.MustCompile(`(?:v|version\s+)?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?)`)

// parseVersion 从 mihomo 版本输出中解析版本号
func parseVersion(output string) string {
	matches := versionRegex.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// DownloadMihomo 下载并安装指定版本的 mihomo
// onProgress 回调会在下载过程中周期性报告进度（status: downloading/extracting）
// 下载地址: https://github.com/MetaCubeX/mihomo/releases/download/v{version}/mihomo-{os}-{arch}-v{version}.gz
func DownloadMihomo(version string, onProgress func(percent int, status string)) (string, error) {
	configDir := GetConfigDir()
	if configDir == "" {
		Errorf("配置目录未设置")
		return "", fmt.Errorf("配置目录未设置")
	}

	osName := runtime.GOOS
	archName := runtime.GOARCH

	// 统一命名
	switch archName {
	case "amd64":
		archName = "amd64"
	case "arm64":
		archName = "arm64"
	}

	fileName := fmt.Sprintf("mihomo-%s-%s-v%s.gz", osName, archName, version)
	downloadURL := fmt.Sprintf("%s/v%s/%s", githubReleaseURL, version, fileName)
	Infof("开始下载 mihomo: %s", downloadURL)

	// 下载到唯一临时文件，避免并发升级或遗留文件互相覆盖。
	tempFile, err := os.CreateTemp("", "mihomo-*.gz")
	if err != nil {
		return "", fmt.Errorf("创建临时下载文件失败: %w", err)
	}
	tmpFile := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return "", fmt.Errorf("关闭临时下载文件失败: %w", err)
	}
	if err := downloadFile(downloadURL, tmpFile, onProgress); err != nil {
		Errorf("下载失败: %v", err)
		if onProgress != nil {
			onProgress(0, "error")
		}
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer os.Remove(tmpFile)

	// 解压到 bin/ 子目录
	if onProgress != nil {
		onProgress(100, "extracting")
	}
	binDir := filepath.Join(configDir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return "", fmt.Errorf("创建 bin 目录失败: %w", err)
	}
	if err := os.Chmod(binDir, 0700); err != nil {
		return "", fmt.Errorf("收紧 bin 目录权限失败: %w", err)
	}
	installPath := filepath.Join(binDir, mihomoBinaryName)
	if runtime.GOOS == "windows" {
		installPath += ".exe"
	}
	Infof("解压 mihomo 到临时安装文件: %s", installPath+".tmp")
	installTmp := installPath + ".tmp"
	_ = os.Remove(installTmp)
	if err := extractGzip(tmpFile, installTmp); err != nil {
		_ = os.Remove(installTmp)
		Errorf("解压失败: %v", err)
		if onProgress != nil {
			onProgress(0, "error")
		}
		return "", fmt.Errorf("解压失败: %w", err)
	}

	// 设置可执行权限并以原子替换方式安装，下载/解压失败不会覆盖已有可用二进制。
	if runtime.GOOS != "windows" {
		if err := os.Chmod(installTmp, 0700); err != nil {
			_ = os.Remove(installTmp)
			return "", fmt.Errorf("设置可执行权限失败: %w", err)
		}
	}
	if err := os.Rename(installTmp, installPath); err != nil {
		_ = os.Remove(installTmp)
		return "", fmt.Errorf("替换 mihomo 二进制失败: %w", err)
	}
	Infof("mihomo 安装完成: %s", installPath)

	// 将安装路径写入配置，便于守护进程后续定位（解决分体启动时用户目录隔离问题）；
	// 通过原子提交持久化，失败时内存与磁盘保持一致。
	if _, err := UpdateGlobalConfig(func(cfg *Config) error {
		cfg.MihomoBinaryPath = installPath
		return nil
	}); err != nil {
		Warnf("保存 mihomo 安装路径失败: %v", err)
	}

	if onProgress != nil {
		onProgress(100, "done")
	}
	return installPath, nil
}

// downloadFile 下载文件到指定路径，onProgress 报告 0-100 的下载进度
func downloadFile(rawURL, dst string, onProgress func(percent int, status string)) error {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return fmt.Errorf("下载请求失败: %s", RedactURLInText(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// 始终写入同目录临时文件，成功后再原子替换目标文件，避免失败下载破坏现有资源。
	tmpPath := dst + ".tmp"
	_ = os.Remove(tmpPath)
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		_ = out.Close()
		if !completed {
			_ = os.Remove(tmpPath)
		}
	}()

	totalSize := resp.ContentLength
	if onProgress == nil || totalSize <= 0 {
		if _, err := io.Copy(out, resp.Body); err != nil {
			return err
		}
	} else {
		var downloaded int64
		buf := make([]byte, DownloadBufferSize)
		lastPercent := -1
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, err := out.Write(buf[:n]); err != nil {
					return err
				}
				downloaded += int64(n)
				percent := int(float64(downloaded) * 100 / float64(totalSize))
				if percent != lastPercent {
					lastPercent = percent
					onProgress(percent, "downloading")
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	completed = true
	return nil
}

// extractGzip 解压 .gz 文件
func extractGzip(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, gr)
	return err
}

// ExternalResourceInfo 外部资源文件信息
type ExternalResourceInfo struct {
	Name    string
	Path    string
	Exists  bool
	Size    int64
	ModTime time.Time
}

// FormatSize 格式化文件大小
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

// CheckExternalResources 检查 mihomo 工作目录下的外部资源文件（大小写不敏感）
func CheckExternalResources() []ExternalResourceInfo {
	mihomoDir := filepath.Join(GetConfigDir(), "mihomo")
	targets := []string{"geoip.dat", "geosite.dat"}

	// 读取目录下所有文件，建立大小写不敏感的文件名映射
	entries, _ := os.ReadDir(mihomoDir)
	fileMap := make(map[string]os.FileInfo) // key: lower(name)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, _ := entry.Info()
		fileMap[strings.ToLower(entry.Name())] = info
	}

	var resources []ExternalResourceInfo
	for _, name := range targets {
		info := ExternalResourceInfo{Name: name}
		if fi, ok := fileMap[strings.ToLower(name)]; ok {
			info.Exists = true
			info.Size = fi.Size()
			info.ModTime = fi.ModTime()
			info.Path = filepath.Join(mihomoDir, fi.Name())
		} else {
			info.Path = filepath.Join(mihomoDir, name)
		}
		resources = append(resources, info)
	}
	return resources
}

// DownloadExternalResources 下载外部资源文件到 mihomo 工作目录
func DownloadExternalResources() error {
	cfg := GlobalConfig()
	mihomoDir := filepath.Join(GetConfigDir(), "mihomo")
	if err := os.MkdirAll(mihomoDir, 0700); err != nil {
		return fmt.Errorf("创建工作目录失败: %w", err)
	}
	if err := os.Chmod(mihomoDir, 0700); err != nil {
		return fmt.Errorf("收紧工作目录权限失败: %w", err)
	}

	resources := []struct {
		name string
		url  string
	}{
		{"geoip.dat", cfg.ExternalResources.GeoIP},
		{"geosite.dat", cfg.ExternalResources.GeoSite},
	}

	for _, r := range resources {
		if r.url == "" {
			continue
		}
		Infof("开始下载外部资源: %s -> %s", r.name, RedactURL(r.url))
		dst := filepath.Join(mihomoDir, r.name)
		if err := downloadFile(r.url, dst, nil); err != nil {
			return fmt.Errorf("下载 %s 失败: %w", r.name, err)
		}
		Infof("外部资源下载完成: %s", dst)
	}
	return nil
}
