package mihomotui

import (
	"fmt"
	"net/http"
	"strings"
)

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
			cfg.SubscriptionPools = append(cfg.SubscriptionPools, SubscriptionPool{ID: id, Name: req.Name, Members: req.Members, ActiveMemberID: active, Enabled: req.Enabled, RefreshInterval: interval})
			return nil
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
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
			p.Name = req.Name
			p.Members = req.Members
			p.ActiveMemberID = req.ActiveMemberID
			p.Enabled = req.Enabled
			if req.RefreshInterval > 0 {
				p.RefreshInterval = req.RefreshInterval
			}
			if p.ActiveMemberID == "" && len(p.Members) > 0 {
				p.ActiveMemberID = p.Members[0]
			}
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
