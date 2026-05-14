package mihomotui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	sysProxyBlockStart = "# >>> mihomo-tui system proxy >>>"
	sysProxyBlockEnd   = "# <<< mihomo-tui system proxy <<<"
)

// SetSystemProxyEnv 开启或关闭系统代理环境变量的持久化注入。
// 开启时向 ~/.bashrc、~/.zshrc、~/.profile 写入 export 语句；
// 关闭时清除这些文件中的 mihomo-tui 标记块。
func (c *Config) SetSystemProxyEnv(enabled bool) error {
	// 先清除所有旧配置（避免重复或残留）
	if err := c.clearSystemProxyEnv(); err != nil {
		return fmt.Errorf("清除旧代理配置失败: %w", err)
	}
	if !enabled {
		Infof("系统代理环境变量已清除")
		return nil
	}

	// 构建代理地址
	httpPort := c.Mihomo.MixedPort
	if httpPort <= 0 {
		httpPort = c.Mihomo.HTTPPort
	}
	if httpPort <= 0 {
		httpPort = 7892
	}
	socksPort := c.Mihomo.SOCKS5Port
	if socksPort <= 0 {
		socksPort = 7891
	}

	httpAddr := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	socksAddr := fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)

	block := fmt.Sprintf("%s\nexport http_proxy=%s\nexport https_proxy=%s\nexport HTTP_PROXY=%s\nexport HTTPS_PROXY=%s\nexport ALL_PROXY=%s\nexport all_proxy=%s\nexport no_proxy=localhost,127.0.0.1,::1\nexport NO_PROXY=localhost,127.0.0.1,::1\n%s\n",
		sysProxyBlockStart,
		httpAddr, httpAddr, httpAddr, httpAddr,
		socksAddr, socksAddr,
		sysProxyBlockEnd,
	)

	files := c.shellConfigFiles()
	for _, f := range files {
		if err := appendToFile(f, block); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("写入 %s 失败: %w", f, err)
		}
	}
	Infof("系统代理环境变量已注入: http=%s socks=%s", httpAddr, socksAddr)
	return nil
}

func (c *Config) clearSystemProxyEnv() error {
	files := c.shellConfigFiles()
	for _, f := range files {
		if err := removeBlockFromFile(f, sysProxyBlockStart, sysProxyBlockEnd); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("清理 %s 失败: %w", f, err)
		}
	}
	return nil
}

func (c *Config) shellConfigFiles() []string {
	return shellConfigFiles()
}

func shellConfigFiles() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	}
}

// CleanupSystemProxyEnv 清理所有 shell 配置文件中的系统代理环境变量标记块。
// systemd stop/uninstall 以 root 运行，需要遍历 /home/* 和 /root 下所有用户的配置文件。
func CleanupSystemProxyEnv() error {
	var allFiles []string

	// 1. root 用户
	allFiles = append(allFiles,
		"/root/.bashrc",
		"/root/.zshrc",
		"/root/.profile",
	)

	// 2. /home/* 下所有普通用户
	entries, err := os.ReadDir("/home")
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			home := filepath.Join("/home", entry.Name())
			allFiles = append(allFiles,
				filepath.Join(home, ".bashrc"),
				filepath.Join(home, ".zshrc"),
				filepath.Join(home, ".profile"),
			)
		}
	}

	// 3. 当前用户（兜底，兼容非 root 直接运行 cleanup）
	home, _ := os.UserHomeDir()
	if home != "" && home != "/root" && !strings.HasPrefix(home, "/home/") {
		allFiles = append(allFiles,
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".zshrc"),
			filepath.Join(home, ".profile"),
		)
	}

	for _, f := range allFiles {
		if err := removeBlockFromFile(f, sysProxyBlockStart, sysProxyBlockEnd); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			Warnf("清理 %s 失败: %v", f, err)
		}
	}
	return nil
}

func appendToFile(path, content string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s := string(data)
	if len(s) > 0 && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += content
	return os.WriteFile(path, []byte(s), 0644)
}

func removeBlockFromFile(path, startMarker, endMarker string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	inBlock := false
	modified := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == startMarker {
			inBlock = true
			modified = true
			continue
		}
		if trimmed == endMarker {
			inBlock = false
			continue
		}
		if !inBlock {
			out = append(out, line)
		}
	}

	if !modified {
		return nil
	}

	// 去除末尾多余空行
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0644)
}
