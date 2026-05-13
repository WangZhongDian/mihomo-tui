package mihomotui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (d *Daemon) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := GlobalConfig()
		writeJSON(w, http.StatusOK, ok(cfg.Subscriptions))
	case http.MethodPost:
		var req SubscriptionImportRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("订阅链接不能为空"))
			return
		}
		if err := d.importSubscription(req.URL); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("导入订阅失败: %w", err))
			return
		}
		writeJSON(w, http.StatusOK, ok(nil))
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}

// importSubscription 下载并导入订阅
func (d *Daemon) importSubscription(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultIPCRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载订阅失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	name := "订阅"

	d.mu.Lock()
	defer d.mu.Unlock()
	cfg := GlobalConfig()
	// 如果已存在同名订阅，更新 URL；否则取一个唯一名称
	idx := cfg.FindSubscriptionByName(name)
	if idx >= 0 {
		cfg.Subscriptions[idx].URL = url
		cfg.Subscriptions[idx].UpdatedAt = time.Now().Format(TimeFormatShort)
	} else {
		// 尝试生成唯一名称
		baseName := name
		for i := 1; i <= 100; i++ {
			if cfg.FindSubscriptionByName(name) < 0 {
				break
			}
			name = fmt.Sprintf("%s%d", baseName, i)
		}
		cfg.Subscriptions = append(cfg.Subscriptions, SubscriptionMeta{
			Name:      name,
			URL:       url,
			UpdatedAt: time.Now().Format(TimeFormatShort),
		})
	}
	if err := cfg.Flush(); err != nil {
		return fmt.Errorf("保存订阅失败: %w", err)
	}
	Infof("订阅导入成功: name=%s url=%s", name, url)
	return nil
}

func (d *Daemon) handleSubscriptionDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/subscriptions/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("缺少订阅名称"))
		return
	}
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case http.MethodPut:
		// 刷新订阅
		writeJSON(w, http.StatusOK, ok("订阅刷新已接收"))
	case http.MethodDelete:
		// 删除订阅
		cfg := GlobalConfig()
		d.mu.Lock()
		newSubs := make([]SubscriptionMeta, 0, len(cfg.Subscriptions))
		for _, sub := range cfg.Subscriptions {
			if sub.Name != name {
				newSubs = append(newSubs, sub)
			}
		}
		cfg.Subscriptions = newSubs
		if err := cfg.Flush(); err != nil {
			Warnf("删除订阅后保存配置失败: %v", err)
		}
		d.mu.Unlock()
		writeJSON(w, http.StatusOK, ok(nil))
	case http.MethodPost:
		if action == "apply" {
			// 应用订阅：先设为激活订阅，再生成配置
			cfg := GlobalConfig()
			d.mu.Lock()
			idx := cfg.FindSubscriptionByName(name)
			if idx < 0 {
				d.mu.Unlock()
				writeError(w, http.StatusBadRequest, fmt.Errorf("订阅不存在: %s", name))
				return
			}
			cfg.ActiveSubscription = idx
			if err := cfg.Flush(); err != nil {
				d.mu.Unlock()
				writeError(w, http.StatusInternalServerError, fmt.Errorf("保存激活订阅失败: %w", err))
				return
			}
			if err := cfg.GenerateMihomoConfig(); err != nil {
				d.mu.Unlock()
				writeError(w, http.StatusInternalServerError, fmt.Errorf("生成配置失败: %w", err))
				return
			}
			// 重新创建 mihomo API 客户端以使用最新的 secret
			d.mihomoAPI = NewMihomoAPIFromConfig()
			d.mu.Unlock()
			writeJSON(w, http.StatusOK, ok(nil))
		} else {
			writeError(w, http.StatusBadRequest, fmt.Errorf("未知操作: %s", action))
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("方法不允许"))
	}
}
