package mihomotui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const subscriptionFailureThreshold = 3
const defaultSubscriptionRefreshInterval = 3600

func normalizedSubscriptionPoolMode(mode SubscriptionPoolMode) SubscriptionPoolMode {
	if mode == "" {
		// Existing pools predate Mode and retain their original primary/backup semantics.
		return SubscriptionPoolModeFailover
	}
	return mode
}

func subscriptionPoolModeLabel(mode SubscriptionPoolMode) string {
	if normalizedSubscriptionPoolMode(mode) == SubscriptionPoolModeMerge {
		return "合并"
	}
	return "优先级主备"
}

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
	if err == nil {
		return !info.IsDir() && info.Size() > 0
	}
	// CacheFile 是绝对路径，在"root 守护进程 + 普通用户 TUI"的分体模式下，
	// 它指向 root 私有的配置目录（0700），TUI 进程 stat 会拿到 EACCES。
	// 此时"读不到"只说明本进程无权判定，并不等于缓存不存在；
	// 真正读该文件生成 provider 的是守护进程。若这里按"不可用"处理，
	// 首页和订阅池页面会把本来正常的订阅报成"不可用"（0/N 参与合并），
	// 而内核里节点其实都还在。因此无权访问时按可用处理，把"缺失"的判断
	// 留给能读到文件的进程（例如守护进程的启动前校验与运行时应用结果）。
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	return false
}

// SubscriptionCacheAvailable 报告订阅是否真的能参与生成 provider。
// UI 与配置生成必须共用同一判定，否则会出现"界面显示 N/N 可用，
// 但生成的 provider 数量更少"的静默不一致。
func SubscriptionCacheAvailable(sub SubscriptionMeta) bool {
	return !sub.Disabled && hasSubscriptionCache(sub)
}

// SkippedPoolMember 描述一个被排除在运行配置之外的池成员。
type SkippedPoolMember struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// PoolMemberAvailability 汇总池成员的实际可用情况。
type PoolMemberAvailability struct {
	Available int                 `json:"available"`
	Total     int                 `json:"total"`
	Skipped   []SkippedPoolMember `json:"skipped,omitempty"`
}

// poolAvailability 计算池中真正参与运行的成员数，并记录每个被跳过成员的原因。
func (c *Config) poolAvailability(pool SubscriptionPool) PoolMemberAvailability {
	avail := PoolMemberAvailability{Total: len(pool.Members)}
	for _, memberID := range pool.Members {
		i := c.FindSubscriptionByID(memberID)
		if i < 0 {
			avail.Skipped = append(avail.Skipped, SkippedPoolMember{ID: memberID, Reason: "订阅已不存在"})
			continue
		}
		sub := c.Subscriptions[i]
		switch {
		case sub.Disabled:
			avail.Skipped = append(avail.Skipped, SkippedPoolMember{ID: memberID, Name: sub.Name, Reason: "订阅已被禁用"})
		case !hasSubscriptionCache(sub):
			avail.Skipped = append(avail.Skipped, SkippedPoolMember{ID: memberID, Name: sub.Name, Reason: "缺少本地缓存"})
		default:
			avail.Available++
		}
	}
	return avail
}

// PoolAvailability 供 UI 复用后端的成员可用性判定，避免前后端对"可用"的定义不一致
// （出现过界面显示 N/N 可用、实际只生成更少 provider 的情况）。
func (c *Config) PoolAvailability(pool SubscriptionPool) PoolMemberAvailability {
	return c.poolAvailability(pool)
}

// subscriptionAffectsRuntime 报告该订阅的变化是否需要重新生成运行配置。
// 合并池的任意成员都参与运行；主备池只有活动成员参与运行；
// 未归入任何已启用订阅池的订阅不影响运行（在订阅页面显示为“未分配”）。
func (c *Config) subscriptionAffectsRuntime(id string) bool {
	if id == "" {
		return false
	}
	for _, pool := range c.SubscriptionPools {
		if !pool.Enabled {
			continue
		}
		if normalizedSubscriptionPoolMode(pool.Mode) == SubscriptionPoolModeMerge {
			for _, memberID := range pool.Members {
				if memberID == id {
					return true
				}
			}
			continue
		}
		if pool.ActiveMemberID == id {
			return true
		}
	}
	return false
}

func (c *Config) FindPoolByIdentifier(id string) int {
	for i := range c.SubscriptionPools {
		if c.SubscriptionPools[i].ID == id || c.SubscriptionPools[i].Name == id {
			return i
		}
	}
	return -1
}

// 说明：历史上存在 defaultPool/ensureDefaultPool，会把新导入的订阅静默塞进
// SubscriptionPools[0]（可能是一个已被禁用、甚至被搬空的池），导致导入看似成功
// 但运行配置只包含第一个订阅。订阅池归属现在完全由用户显式决定，
// 未归入任何池的订阅在订阅页面显示为"未分配"，因此不再提供默认池入口。
func (c *Config) activeSubscriptionForPool(pool SubscriptionPool) (SubscriptionMeta, bool) {
	i := c.FindSubscriptionByID(pool.ActiveMemberID)
	if i < 0 || c.Subscriptions[i].Disabled {
		return SubscriptionMeta{}, false
	}
	return c.Subscriptions[i], true
}
func (c *Config) activePoolSubscriptions() ([]SubscriptionMeta, error) {
	result := make([]SubscriptionMeta, 0, len(c.SubscriptionPools))
	enabledPools := 0
	for _, pool := range c.SubscriptionPools {
		if !pool.Enabled {
			continue
		}
		enabledPools++
		if normalizedSubscriptionPoolMode(pool.Mode) == SubscriptionPoolModeMerge {
			availability := c.poolAvailability(pool)
			for _, skipped := range availability.Skipped {
				// 静默减员会让"合并池"看起来只剩第一个订阅，必须留下可追溯的原因。
				Warnf("合并订阅池 %q 跳过成员: name=%s reason=%s", pool.Name, skipped.Name, skipped.Reason)
			}
			available := 0
			for _, memberID := range pool.Members {
				i := c.FindSubscriptionByID(memberID)
				if i < 0 {
					continue
				}
				sub := c.Subscriptions[i]
				if !SubscriptionCacheAvailable(sub) {
					continue
				}
				result = append(result, sub)
				available++
			}
			if available == 0 {
				return nil, fmt.Errorf("合并订阅池 %q 没有可用本地缓存（成员 %d 个，全部被跳过）", pool.Name, len(pool.Members))
			}
			continue
		}

		sub, ok := c.activeSubscriptionForPool(pool)
		if !ok {
			return nil, fmt.Errorf("订阅池 %q 没有有效活动源", pool.Name)
		}
		if !SubscriptionCacheAvailable(sub) {
			return nil, fmt.Errorf("订阅池 %q 的活动源 %q 没有可用本地缓存", pool.Name, sub.Name)
		}
		result = append(result, sub)
	}
	if len(result) == 0 {
		// 区分"没有池"、"池全被禁用"和"池启用了但没有可用缓存"，
		// 让 UI 能直接告诉用户该去做什么，而不是笼统报错。
		switch {
		case len(c.SubscriptionPools) == 0:
			return nil, fmt.Errorf("没有订阅池：请先在订阅池页面创建订阅池并加入订阅源")
		case enabledPools == 0:
			return nil, fmt.Errorf("没有已启用的订阅池：%d 个订阅池全部处于禁用状态，请在订阅池页面启用", len(c.SubscriptionPools))
		default:
			return nil, fmt.Errorf("已启用的订阅池没有可用的订阅缓存，请先刷新订阅")
		}
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
