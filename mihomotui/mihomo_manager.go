package mihomotui

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

const (
	mihomoRepo       = "MetaCubeX/mihomo"
	githubAPIURL     = "https://api.github.com/repos/" + mihomoRepo + "/releases/latest"
	githubReleaseURL = "https://github.com/" + mihomoRepo + "/releases/download"
	mihomoBinaryName = "mihomo"
	httpTimeout      = 30 * time.Second
)

// GetMihomoLastVersion 从 GitHub API 获取最新版本号（如 v1.19.0）
func GetMihomoLastVersion() (string, error) {
	// Keep the historical API while delegating to the release catalog. This is
	// more robust than /releases/latest and validates that an asset exists for
	// the current platform.
	versions, _, err := fetchMihomoReleaseCatalog()
	if err != nil {
		return "", err
	}
	for _, version := range versions {
		if !version.Prerelease {
			return version.Version, nil
		}
	}
	return "", fmt.Errorf("未找到适用于当前平台的稳定 mihomo 版本")
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
	return getMihomoBinaryVersion(binaryPath)
}

// getMihomoBinaryVersion reads the actual version from a specific executable.
// 启动器使用该函数记录真正启动的二进制版本，避免把“选中的版本”误当作运行版本。
func getMihomoBinaryVersion(binaryPath string) (string, error) {
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
	Infof("mihomo 二进制版本: %s", version)
	return normalizeMihomoVersion(version), nil
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

// DownloadProgress is reported while a file is transferred. TotalBytes is -1
// when the server/proxy omits Content-Length.
type DownloadProgress struct {
	Percent         int
	Status          string
	DownloadedBytes int64
	TotalBytes      int64
}

// downloadFile keeps the original lightweight callback API for resource downloads.
func downloadFile(rawURL, dst string, onProgress func(percent int, status string)) error {
	return downloadFileWithProgress(rawURL, dst, func(p DownloadProgress) {
		if onProgress != nil {
			onProgress(p.Percent, p.Status)
		}
	})
}

func downloadFileWithProgress(rawURL, dst string, onProgress func(DownloadProgress)) error {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return fmt.Errorf("下载请求失败: %s", RedactURLInText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
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
	var downloaded int64
	lastPercent := -2
	lastBytes := int64(-1)
	buf := make([]byte, DownloadBufferSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
			percent := 0
			if totalSize > 0 {
				percent = int(downloaded * 100 / totalSize)
				if percent > 100 {
					percent = 100
				}
			}
			// Known-length transfers report on each percent. Unknown-length
			// transfers report byte progress at least every 256 KiB.
			if onProgress != nil && ((totalSize > 0 && percent != lastPercent) || (totalSize <= 0 && downloaded-lastBytes >= 256<<10)) {
				onProgress(DownloadProgress{Percent: percent, Status: "downloading", DownloadedBytes: downloaded, TotalBytes: totalSize})
				lastPercent = percent
				lastBytes = downloaded
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if onProgress != nil && totalSize <= 0 && downloaded != lastBytes {
		onProgress(DownloadProgress{Percent: 0, Status: "downloading", DownloadedBytes: downloaded, TotalBytes: totalSize})
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
