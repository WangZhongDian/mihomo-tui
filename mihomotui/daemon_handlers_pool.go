package mihomotui

import (
	"fmt"
	"net/http"
	"strings"
)

// assignPoolMembers 将成员独占地归入目标池；从其他池移除后，空池会自动禁用并保留。
// 订阅池的成员关系是“移动”而不是“复制”，避免创建新池时触发重复归属校验。
func assignPoolMembers(cfg *Config, target int, members []string) {
	wanted := make(map[string]bool, len(members))
	for _, id := range members {
		wanted[id] = true
	}
	for i := range cfg.SubscriptionPools {
		if i == target {
			continue
		}
		pool := &cfg.SubscriptionPools[i]
		kept := pool.Members[:0]
		for _, id := range pool.Members {
			if !wanted[id] {
				kept = append(kept, id)
			}
		}
		pool.Members = kept
		if !wanted[pool.ActiveMemberID] {
			continue
		}
		if len(pool.Members) > 0 {
			pool.ActiveMemberID = pool.Members[0]
		} else {
			pool.ActiveMemberID = ""
			pool.Enabled = false
			pool.Degraded = true
			pool.LastSwitchAt = timestampNow()
			pool.LastSwitchReason = "成员已移入其他订阅池"
		}
	}
}

func (d *Daemon) handleSubscriptionPools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.SubscriptionPools))
	case http.MethodPost:
		var req SubscriptionPoolRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		var id string
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			if strings.TrimSpace(req.Name) == "" {
				return fmt.Errorf("订阅池名称不能为空")
			}
			if cfg.FindPoolByIdentifier(req.Name) >= 0 {
				return fmt.Errorf("订阅池名称已存在: %s", req.Name)
			}
			id = newSubscriptionID()
			active := req.ActiveMemberID
			if active == "" && len(req.Members) > 0 {
				active = req.Members[0]
			}
			interval := req.RefreshInterval
			if interval == 0 {
				interval = defaultSubscriptionRefreshInterval
			}
			mode := normalizedSubscriptionPoolMode(req.Mode)
			if mode != SubscriptionPoolModeFailover && mode != SubscriptionPoolModeMerge {
				return fmt.Errorf("订阅池运行模式非法: %q（可选 failover/merge）", req.Mode)
			}
			cfg.SubscriptionPools = append(cfg.SubscriptionPools, SubscriptionPool{ID: id, Name: req.Name, Mode: mode, Members: req.Members, ActiveMemberID: active, Enabled: req.Enabled, RefreshInterval: interval})
			assignPoolMembers(cfg, len(cfg.SubscriptionPools)-1, req.Members)
			return nil
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if report := d.reconcileLatest("subscription-pool-create"); !report.Applied {
			Warnf("新建订阅池应用失败: %s", report.Err)
		}
		writeJSON(w, http.StatusOK, ok(map[string]string{"id": id}))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
func (d *Daemon) handleSubscriptionPoolDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/subscription-pools/"), "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("订阅池 ID 不能为空"))
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req SubscriptionPoolRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			i := cfg.FindPoolByIdentifier(id)
			if i < 0 {
				return fmt.Errorf("订阅池不存在")
			}
			if strings.TrimSpace(req.Name) == "" {
				return fmt.Errorf("订阅池名称不能为空")
			}
			for j, existing := range cfg.SubscriptionPools {
				if j != i && existing.Name == req.Name {
					return fmt.Errorf("订阅池名称已存在: %s", req.Name)
				}
			}
			p := &cfg.SubscriptionPools[i]
			mode := normalizedSubscriptionPoolMode(req.Mode)
			if mode != SubscriptionPoolModeFailover && mode != SubscriptionPoolModeMerge {
				return fmt.Errorf("订阅池运行模式非法: %q（可选 failover/merge）", req.Mode)
			}
			p.Name = req.Name
			p.Mode = mode
			p.Members = req.Members
			p.ActiveMemberID = req.ActiveMemberID
			p.Enabled = req.Enabled
			if req.RefreshInterval > 0 {
				p.RefreshInterval = req.RefreshInterval
			}
			if p.ActiveMemberID == "" && len(p.Members) > 0 {
				p.ActiveMemberID = p.Members[0]
			}
			assignPoolMembers(cfg, i, p.Members)
			return nil
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if report := d.reconcileLatest("subscription-pool-update"); !report.Applied {
			Warnf("订阅池更新应用失败: %s", report.Err)
		}
		writeJSON(w, http.StatusOK, ok(nil))
	case http.MethodDelete:
		_, err := UpdateGlobalConfig(func(cfg *Config) error {
			i := cfg.FindPoolByIdentifier(id)
			if i < 0 {
				return fmt.Errorf("订阅池不存在")
			}
			cfg.SubscriptionPools = append(cfg.SubscriptionPools[:i], cfg.SubscriptionPools[i+1:]...)
			return nil
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	case http.MethodPost:
		if !strings.HasSuffix(r.URL.Path, "/refresh") {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作"))
			return
		}
		cfg := GlobalConfig()
		i := cfg.FindPoolByIdentifier(strings.TrimSuffix(id, "/refresh"))
		if i < 0 {
			writeError(w, http.StatusNotFound, fmt.Errorf("订阅池不存在"))
			return
		}
		var failures []string
		for _, member := range cfg.SubscriptionPools[i].Members {
			if err := d.refreshSubscription(member); err != nil {
				failures = append(failures, RedactURLInText(err.Error()))
			}
		}
		if len(failures) > 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("订阅池刷新完成，但 %d 个成员失败: %s", len(failures), strings.Join(failures, "; ")))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
