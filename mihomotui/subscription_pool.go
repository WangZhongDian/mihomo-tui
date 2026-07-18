package mihomotui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const subscriptionFailureThreshold = 3
const defaultSubscriptionRefreshInterval = 3600

func subscriptionCacheDir() string {
	dir := filepath.Join(GetSubscriptionsDir(), "cache")
	_ = os.MkdirAll(dir, 0700)
	_ = os.Chmod(dir, 0700)
	return dir
}

func subscriptionCachePath(id string) string {
	return filepath.Join(subscriptionCacheDir(), id+".yaml")
}

func writeSubscriptionCache(id string, content []byte) (string, string, error) {
	if err := validateSubscriptionContent(content); err != nil {
		return "", "", err
	}
	dir := subscriptionCacheDir()
	if dir == "" {
		return "", "", fmt.Errorf("创建订阅缓存目录失败")
	}
	target := subscriptionCachePath(id)
	tmp, err := os.CreateTemp(dir, "."+id+"-*")
	if err != nil {
		return "", "", fmt.Errorf("创建订阅缓存临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return "", "", err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return "", "", fmt.Errorf("写入订阅缓存失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", "", fmt.Errorf("原子替换订阅缓存失败: %w", err)
	}
	_ = os.Chmod(target, 0600)
	sum := sha256.Sum256(content)
	return target, hex.EncodeToString(sum[:]), nil
}

func hasSubscriptionCache(sub SubscriptionMeta) bool {
	if sub.CacheFile == "" {
		return false
	}
	info, err := os.Stat(sub.CacheFile)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (c *Config) FindPoolByIdentifier(id string) int {
	for i := range c.SubscriptionPools {
		if c.SubscriptionPools[i].ID == id || c.SubscriptionPools[i].Name == id {
			return i
		}
	}
	return -1
}
func (c *Config) defaultPool() *SubscriptionPool {
	if len(c.SubscriptionPools) == 0 {
		return nil
	}
	return &c.SubscriptionPools[0]
}
func (c *Config) ensureDefaultPool() *SubscriptionPool {
	if p := c.defaultPool(); p != nil {
		return p
	}
	p := SubscriptionPool{ID: newSubscriptionID(), Name: "默认订阅池", Enabled: true, RefreshInterval: defaultSubscriptionRefreshInterval}
	c.SubscriptionPools = append(c.SubscriptionPools, p)
	return &c.SubscriptionPools[len(c.SubscriptionPools)-1]
}
func (c *Config) activeSubscriptionForPool(pool SubscriptionPool) (SubscriptionMeta, bool) {
	i := c.FindSubscriptionByID(pool.ActiveMemberID)
	if i < 0 {
		return SubscriptionMeta{}, false
	}
	return c.Subscriptions[i], true
}
func (c *Config) activePoolSubscriptions() ([]SubscriptionMeta, error) {
	result := make([]SubscriptionMeta, 0, len(c.SubscriptionPools))
	for _, pool := range c.SubscriptionPools {
		if !pool.Enabled {
			continue
		}
		sub, ok := c.activeSubscriptionForPool(pool)
		if !ok {
			return nil, fmt.Errorf("订阅池 %q 没有有效活动源", pool.Name)
		}
		if !hasSubscriptionCache(sub) {
			return nil, fmt.Errorf("订阅池 %q 的活动源 %q 没有可用本地缓存", pool.Name, sub.Name)
		}
		result = append(result, sub)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("没有已启用且可用的订阅池")
	}
	return result, nil
}
func timestampNow() string { return time.Now().Format(TimeFormatShort) }
func normalizedSource(t SubscriptionSource) SubscriptionSource {
	if t == "" {
		return SubscriptionSourceURL
	}
	return t
}
func sourceLabel(s SubscriptionMeta) string {
	if s.SourceType == SubscriptionSourceContent {
		return "粘贴内容"
	}
	if s.SourceType == SubscriptionSourceFile {
		return "本地文件"
	}
	return strings.TrimSpace(s.URL)
}
