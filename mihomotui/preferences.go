package mihomotui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ClientPreferencesFileName 客户端偏好文件名（与 daemon 权威配置 config.yaml 分离）。
const ClientPreferencesFileName = "preferences.yaml"

// ClientPreferences 当前 TUI 用户的本地偏好（P1 第 7 节：客户端/服务端配置边界）。
// 这些设置属于"发起操作的系统用户"，不写入 daemon 全局配置，
// 避免多用户共享 root daemon 时互相覆盖。
type ClientPreferences struct {
	// SystemProxy 本用户的系统代理开关。开启时写入当前用户的 shell
	// 环境变量（~/.bashrc 等），只影响本用户会话。
	SystemProxy bool `yaml:"system_proxy"`
}

var (
	clientPrefs       ClientPreferences
	clientPrefsLoaded bool
	clientPrefsMu     sync.RWMutex
)

// preferencesFilePath 返回客户端偏好文件路径。
//
// 注意 `sudo mihomo-tui` 场景：此时 GetConfigDir() 指向 /root/.config/mihomo-tui，
// 但系统代理注入的目标是发起 sudo 的用户主目录。若偏好也落到 /root，
// 下次以普通用户启动就读不到它，会被判定为"关闭"并清掉注入块。
// 因此 root + SUDO_USER 时把偏好放在目标用户的配置目录里。
func preferencesFilePath() string {
	return filepath.Join(clientPreferencesDir(), ClientPreferencesFileName)
}

// clientPreferencesDir 返回客户端偏好所在目录。
// 普通用户即 GetConfigDir()；root 代 sudo 用户操作时改为该用户自己的配置目录。
func clientPreferencesDir() string {
	if os.Geteuid() == 0 {
		if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
			if home := homeDirOfUser(sudoUser); home != "" {
				return filepath.Join(home, ".config", ConfigDirName)
			}
		}
	}
	return GetConfigDir()
}

// readClientPreferences 从磁盘读取客户端偏好；文件不存在时返回 nil（由调用方迁移）。
func readClientPreferences() (*ClientPreferences, error) {
	data, err := os.ReadFile(preferencesFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取客户端偏好失败: %w", err)
	}
	var prefs ClientPreferences
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return nil, fmt.Errorf("解析客户端偏好失败: %w", err)
	}
	return &prefs, nil
}

// ensureClientPreferencesLoaded 惰性加载客户端偏好。
// 首次加载且偏好文件不存在时，从全局配置迁移 system_proxy（旧版本该开关
// 存储在共享配置中），保证升级后开关状态不丢失。
func ensureClientPreferencesLoaded() {
	clientPrefsMu.Lock()
	defer clientPrefsMu.Unlock()
	if clientPrefsLoaded {
		return
	}
	clientPrefsLoaded = true
	prefs, err := readClientPreferences()
	if err != nil {
		Warnf("加载客户端偏好失败，使用默认值: %v", err)
		return
	}
	if prefs == nil {
		clientPrefs = ClientPreferences{SystemProxy: GlobalConfig().System.SystemProxy}
		return
	}
	clientPrefs = *prefs
}

// saveClientPreferencesLocked 原子写入偏好文件（临时文件 + 重命名，0600）。
// 调用方需持有 clientPrefsMu。
func saveClientPreferencesLocked() error {
	path := preferencesFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建偏好目录失败: %w", err)
	}
	data, err := yaml.Marshal(&clientPrefs)
	if err != nil {
		return fmt.Errorf("序列化客户端偏好失败: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("写入客户端偏好失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换客户端偏好文件失败: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("收紧客户端偏好权限失败: %w", err)
	}
	// root 代写普通用户的偏好文件后，把所有者改回去。
	chownToSystemProxyTarget(path)
	return nil
}

// ClientPrefs 返回当前用户的客户端偏好快照（线程安全）。
func ClientPrefs() ClientPreferences {
	ensureClientPreferencesLoaded()
	clientPrefsMu.RLock()
	defer clientPrefsMu.RUnlock()
	return clientPrefs
}

// GetSystemProxyPreference 返回本用户的系统代理开关状态。
func GetSystemProxyPreference() bool {
	return ClientPrefs().SystemProxy
}

// SetSystemProxyPreference 设置本用户的系统代理开关并持久化。
// 仅影响当前用户；不再写入 daemon 全局配置。
func SetSystemProxyPreference(enabled bool) error {
	ensureClientPreferencesLoaded()
	clientPrefsMu.Lock()
	defer clientPrefsMu.Unlock()
	if clientPrefs.SystemProxy == enabled {
		return nil
	}
	clientPrefs.SystemProxy = enabled
	if err := saveClientPreferencesLocked(); err != nil {
		return err
	}
	Infof("系统代理偏好已保存: enabled=%v", enabled)
	return nil
}

// resetClientPreferencesForTest 重置偏好缓存（仅测试使用）。
func resetClientPreferencesForTest() {
	clientPrefsMu.Lock()
	defer clientPrefsMu.Unlock()
	clientPrefs = ClientPreferences{}
	clientPrefsLoaded = false
}
