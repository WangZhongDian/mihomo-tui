package mihomotui

import (
	"errors"
	"fmt"
	"os"
)

// ErrConfigConflict 配置版本冲突：客户端基于过期版本提交整份配置，
// 说明配置已被其他会话修改。客户端应重新获取最新配置后重试。
var ErrConfigConflict = errors.New("配置版本冲突")

// Clone 返回配置的深拷贝（切片字段独立，修改副本不影响原配置）。
func (c *Config) Clone() Config {
	cp := *c
	cp.Subscriptions = append([]SubscriptionMeta(nil), c.Subscriptions...)
	cp.MihomoVersions = append([]MihomoVersionInfo(nil), c.MihomoVersions...)
	cp.SubscriptionPools = append([]SubscriptionPool(nil), c.SubscriptionPools...)
	for i := range cp.SubscriptionPools {
		cp.SubscriptionPools[i].Members = append([]string(nil), c.SubscriptionPools[i].Members...)
	}
	cp.RuleProviderSubscriptions = append([]RuleProviderSubscription(nil), c.RuleProviderSubscriptions...)
	cp.CustomRules = append([]string(nil), c.CustomRules...)
	return cp
}

// commitConfigLocked 在持有 configMu 的前提下执行原子提交：
// 校验 → 原子落盘 → 替换内存快照。任何一步失败时内存与磁盘均保持旧值。
func commitConfigLocked(next Config) (Config, error) {
	if err := next.Validate(); err != nil {
		return Config{}, err
	}
	next.Version = globalConfig.Version + 1
	// 先落盘：Flush 失败时内存尚未变更，磁盘通过原子重命名也只可能是旧值。
	if err := next.Flush(); err != nil {
		return Config{}, err
	}
	globalConfig = next
	Infof("配置已提交: version=%d", next.Version)
	return next.Clone(), nil
}

// UpdateGlobalConfig 原子地更新全局配置：
// 克隆当前快照 → 执行 fn 修改 → 校验 → 原子落盘 → 替换内存。
// fn 返回错误、校验失败或落盘失败时，内存与磁盘均保持旧值。
// 返回提交后的最新快照。
func UpdateGlobalConfig(fn func(*Config) error) (Config, error) {
	if fn == nil {
		return Config{}, fmt.Errorf("配置更新函数不能为空")
	}
	configMu.Lock()
	defer configMu.Unlock()
	next := globalConfig.Clone()
	if err := fn(&next); err != nil {
		return Config{}, err
	}
	return commitConfigLocked(next)
}

// daemonRunsAsRoot 报告当前进程是否为 root daemon（多用户共享场景）。
// 提取为变量以便测试替换。
var daemonRunsAsRoot = func() bool { return os.Geteuid() == 0 }

// ReplaceGlobalConfig 用 req 整份替换全局配置（daemon 全量配置提交入口）。
// 携带乐观并发控制：req.Version 必须与当前版本一致，否则返回 ErrConfigConflict；
// 新安装的配置版本为 0，客户端读取后即可获得当前版本。
// 字段边界（P1 第 7 节）：
//   - Secret：req 中为空时保留当前值（常规 /config 响应不携带 Secret）。
//   - root daemon 的本机路径字段（mihomo 配置/二进制路径、日志目录）属于服务端环境，
//     不接受客户端提交，避免共享 daemon 的不同用户互相覆盖本机路径。
func ReplaceGlobalConfig(req Config) (Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
	if req.Version != globalConfig.Version {
		return Config{}, fmt.Errorf("%w: 配置已被其他会话修改（当前版本 %d，提交基于版本 %d），请刷新后重试",
			ErrConfigConflict, globalConfig.Version, req.Version)
	}
	next := req.Clone()
	if next.Mihomo.Secret == "" {
		next.Mihomo.Secret = globalConfig.Mihomo.Secret
	}
	if daemonRunsAsRoot() {
		next.MihomoConfigPath = globalConfig.MihomoConfigPath
		next.MihomoBinaryPath = globalConfig.MihomoBinaryPath
		next.LogDir = globalConfig.LogDir
	}
	return commitConfigLocked(next)
}
