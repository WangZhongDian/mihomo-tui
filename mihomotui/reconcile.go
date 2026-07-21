package mihomotui

import (
	"fmt"
	"time"
)

// ApplyReport 描述一次"配置已提交后"的运行时应用结果。
// 配置本身已经持久化成功；ApplyReport 只反映生成/重载/重启/TUN 同步等
// 运行时阶段是否成功，便于 UI 区分"保存失败"与"保存成功但应用失败"。
type ApplyReport struct {
	Applied bool   `json:"applied"`         // 全部阶段是否成功
	Stage   string `json:"stage,omitempty"` // 失败阶段: generate / reload / restart / tun / timeout
	Err     string `json:"err,omitempty"`   // 失败原因（面向用户）
}

// reconcileRequest 一次串行化的运行时应用请求。
type reconcileRequest struct {
	reason  string
	oldTUN  bool // 提交前的 TUN 状态（仅配置提交需要）
	newTUN  bool // 提交后的 TUN 状态
	syncTUN bool // 是否需要按 TUN 状态变化同步路由规则
	result  chan ApplyReport
}

// reconcileApplyFunc 是可注入的应用实现，便于测试替换。
type reconcileApplyFunc func(req reconcileRequest) ApplyReport

// ensureReconciler 惰性启动串行应用工作协程（零值 Daemon 亦可安全使用）。
func (d *Daemon) ensureReconciler() {
	d.reconcileOnce.Do(func() {
		d.reconcileCh = make(chan reconcileRequest, 16)
		go d.reconcileLoop()
	})
}

// reconcileLoop 串行消费应用请求：配置应用任务在 daemon 内严格逐个执行，
// 且每个任务执行时都读取最新已提交配置，快速连续修改不会出现旧配置覆盖新配置。
func (d *Daemon) reconcileLoop() {
	for req := range d.reconcileCh {
		func() {
			defer func() {
				if r := recover(); r != nil {
					Errorf("配置应用任务发生 panic: reason=%s panic=%v", req.reason, r)
					if req.result != nil {
						req.result <- ApplyReport{Applied: false, Stage: "panic", Err: fmt.Sprintf("内部错误: %v", r)}
					}
				}
			}()
			apply := d.reconcileApply
			if apply == nil {
				apply = d.runReconcile
			}
			report := apply(req)
			if report.Applied {
				Infof("配置应用完成: reason=%s", req.reason)
			} else {
				Warnf("配置应用失败: reason=%s stage=%s err=%s", req.reason, report.Stage, report.Err)
			}
			if req.result != nil {
				req.result <- report
			}
		}()
	}
}

// submitReconcile 提交一个应用请求并同步等待结果（带超时）。
// 超时仅意味着结果未在等待窗口内返回，任务本身仍在后台继续执行。
func (d *Daemon) submitReconcile(req reconcileRequest, waitTimeout time.Duration) ApplyReport {
	d.ensureReconciler()
	req.result = make(chan ApplyReport, 1)
	d.reconcileCh <- req
	select {
	case report := <-req.result:
		return report
	case <-time.After(waitTimeout):
		return ApplyReport{Applied: false, Stage: "timeout", Err: "应用超时，任务仍在后台继续执行"}
	}
}

// reconcileConfigChange 在配置提交后串行应用运行时变更。
func (d *Daemon) reconcileConfigChange(reason string, oldTUN, newTUN bool) ApplyReport {
	return d.submitReconcile(reconcileRequest{
		reason:  reason,
		oldTUN:  oldTUN,
		newTUN:  newTUN,
		syncTUN: oldTUN != newTUN,
	}, 20*time.Second)
}

// reconcileLatest 按最新已提交配置重新生成并热重载（订阅/规则订阅变更后调用）。
func (d *Daemon) reconcileLatest(reason string) ApplyReport {
	return d.submitReconcile(reconcileRequest{reason: reason}, 20*time.Second)
}

// runReconcile 实际执行一次运行时应用：
// 生成 mihomo YAML → （运行中则热重载，失败则重启）→ 按需同步 TUN 路由。
func (d *Daemon) runReconcile(req reconcileRequest) ApplyReport {
	// 1. 生成 mihomo 配置（基于最新已提交配置快照）
	cfg := GlobalConfig()
	if err := cfg.GenerateMihomoConfig(); err != nil {
		return ApplyReport{Applied: false, Stage: "generate", Err: fmt.Sprintf("生成 mihomo 配置失败: %v", err)}
	}

	// 2. 从关闭切到开启 TUN 时，必须在热重载让 mihomo 接管流量前先
	// 安装回包修复。失败则不热重载，保持旧的安全运行状态。
	d.mu.Lock()
	d.mihomoAPI = NewMihomoAPIFromConfig()
	api := d.mihomoAPI
	process := d.mihomoProcess
	d.mu.Unlock()

	running := process != nil && process.IsRunning()
	if req.syncTUN && req.newTUN && running {
		if err := SetupTUNRouting(); err != nil {
			return ApplyReport{Applied: false, Stage: "tun", Err: fmt.Sprintf("TUN 路由修复预检/设置失败，未启用 TUN: %v", err)}
		}
	}
	if running {
		if api == nil {
			return ApplyReport{Applied: false, Stage: "reload", Err: "mihomo API 客户端未初始化"}
		}
		if err := api.ReloadConfigs(true); err != nil {
			Warnf("热重载 mihomo 失败，尝试重启: %v", err)
			if rerr := process.Restart(); rerr != nil {
				return ApplyReport{Applied: false, Stage: "restart",
					Err: fmt.Sprintf("热重载失败: %v；重启失败: %v", err, rerr)}
			}
		}
	}

	// 3. 关闭 TUN 时，配置已成功热重载/重启为非 TUN 模式后再清理项目规则。
	if req.syncTUN && !req.newTUN && running {
		if err := RestoreTUNRouting(); err != nil {
			return ApplyReport{Applied: false, Stage: "tun", Err: fmt.Sprintf("TUN 路由规则清理失败: %v", err)}
		}
	}

	return ApplyReport{Applied: true}
}
