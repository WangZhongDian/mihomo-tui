package mihomotui

import (
	"context"
	"time"
)

// startSubscriptionScheduler 由 daemon 主动刷新远端订阅；它不会删除最后成功缓存。
func (d *Daemon) startSubscriptionScheduler() {
	if d.subscriptionSchedulerCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.subscriptionSchedulerCancel = cancel
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		// 启动后稍候检查一次，避免阻塞 daemon socket 建立。
		select {
		case <-time.After(5 * time.Second):
			d.refreshDueSubscriptionPools()
		case <-ctx.Done():
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.refreshDueSubscriptionPools()
			}
		}
	}()
}
func (d *Daemon) refreshDueSubscriptionPools() {
	cfg := GlobalConfig()
	now := time.Now()
	for _, pool := range cfg.SubscriptionPools {
		if !pool.Enabled {
			continue
		}
		interval := pool.RefreshInterval
		if interval <= 0 {
			interval = defaultSubscriptionRefreshInterval
		}
		for _, id := range pool.Members {
			i := cfg.FindSubscriptionByID(id)
			if i < 0 {
				continue
			}
			sub := cfg.Subscriptions[i]
			if normalizedSource(sub.SourceType) != SubscriptionSourceURL {
				continue
			}
			due := true
			if sub.ProfileUpdateInterval > 0 { // profile-update-interval is defined in hours by Clash/Mihomo providers.
				interval = sub.ProfileUpdateInterval * 3600
			}
			if sub.LastCheckedAt != "" {
				if t, err := time.Parse(TimeFormatShort, sub.LastCheckedAt); err == nil {
					due = now.Sub(t) >= time.Duration(interval)*time.Second
				}
			}
			if due {
				if err := d.refreshSubscription(id); err != nil {
					Warnf("后台刷新订阅失败: name=%s err=%s", sub.Name, RedactURLInText(err.Error()))
				}
			}
		}
	}
	d.syncSubscriptionMetadataFromProviders()
}
