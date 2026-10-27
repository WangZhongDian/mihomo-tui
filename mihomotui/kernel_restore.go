package mihomotui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// KernelWasRunningEnv 安装/更新脚本可在 stop 服务前设置，供 install_service 恢复内核。
// 合法值: "1"/"true"/"yes" 表示装前内核在跑；"0"/"false"/"no" 表示未跑。
// 未设置时 install_service 会自行探测。
const KernelWasRunningEnv = "MIHOMO_TUI_KERNEL_WAS_RUNNING"

// ParseKernelWasRunningHint 解析安装脚本或 CLI 传入的内核运行快照。
// ok=false 表示未提供有效提示，调用方应自行探测。
func ParseKernelWasRunningHint(raw string) (wasRunning bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// ResolveKernelWasRunning 优先使用 hint/环境变量；否则在 stop 前探测当前 IPC 状态。
func ResolveKernelWasRunning(hint string) bool {
	if wasRunning, ok := ParseKernelWasRunningHint(hint); ok {
		return wasRunning
	}
	if wasRunning, ok := ParseKernelWasRunningHint(os.Getenv(KernelWasRunningEnv)); ok {
		return wasRunning
	}
	running, err := ProbeKernelRunning()
	if err != nil {
		Warnf("探测 mihomo 内核状态失败，安装后不自动恢复内核: %v", err)
		return false
	}
	return running
}

// ProbeKernelRunning 通过 root/共享 IPC 查询 mihomo 内核是否在运行。
// daemon 未就绪或不可达时返回 (false, nil)，不把“服务未起”当作错误。
func ProbeKernelRunning() (bool, error) {
	if err := IPCProbeDaemon(); err != nil {
		return false, nil
	}
	client, err := NewIPCClient()
	if err != nil {
		return false, nil
	}
	status, err := client.IPCGetMihomoStatus()
	if err != nil {
		return false, fmt.Errorf("查询 mihomo 状态失败: %w", err)
	}
	return status.Running, nil
}

// WaitDaemonReady 等待 IPC 守护进程就绪。
func WaitDaemonReady(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultDaemonWaitTimeout
	}
	// 安装后首次启动可能略慢于默认 TUI 等待时间。
	if timeout < 15*time.Second {
		timeout = 15 * time.Second
	}
	return IPCWaitForDaemon(timeout)
}

// RestoreKernelIfNeeded 在安装/更新完成后按快照恢复 mihomo 内核。
// 恢复失败只返回 error，由调用方决定是否让安装整体失败（通常只警告）。
func RestoreKernelIfNeeded(wasRunning bool) error {
	if !wasRunning {
		return nil
	}
	if err := WaitDaemonReady(15 * time.Second); err != nil {
		return fmt.Errorf("等待守护进程就绪失败，无法恢复内核: %w", err)
	}
	// 若内核已在跑（极少见），直接成功。
	running, err := ProbeKernelRunning()
	if err != nil {
		return err
	}
	if running {
		Infof("mihomo 内核已在运行，跳过恢复启动")
		return nil
	}
	client, err := NewIPCClient()
	if err != nil {
		return fmt.Errorf("连接 IPC 失败，无法恢复内核: %w", err)
	}
	if err := client.IPCStartMihomo(); err != nil {
		return fmt.Errorf("恢复启动 mihomo 内核失败: %w", err)
	}
	Infof("已恢复启动 mihomo 内核")
	return nil
}
