package mihomotui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrMihomoNotRunning 在要求停止/重启一个未运行的 mihomo 进程时返回。
var ErrMihomoNotRunning = errors.New("mihomo 未在运行")

const (
	defaultStopTimeout = 5 * time.Second
	defaultStartSettle = 500 * time.Millisecond
	processOutputLimit = 8 << 10 // 进程输出诊断缓冲上限
)

// cappedBuffer 线程安全的定长缓冲：超出上限时丢弃最旧的内容，用于保留进程退出前的输出尾部。
type cappedBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.limit:]...)
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// MihomoProcess mihomo 内核进程管理器
type MihomoProcess struct {
	mu      sync.RWMutex
	cmd     *exec.Cmd
	running bool
	pid     int
	// exited 由 Wait goroutine 在进程退出后投递（buffered 1），供 Start/Stop 等待状态变化。
	exited chan error
	// output 保留进程最近的 stdout/stderr 尾部，用于启动失败诊断。
	output *cappedBuffer
	// stopTimeout 停止等待 SIGTERM 生效的最长时间，超时后 SIGKILL。
	stopTimeout time.Duration
	// startSettle 启动后的存活确认窗口：窗口内退出视为启动失败。
	startSettle time.Duration
}

// NewMihomoProcess 创建进程管理器
func NewMihomoProcess() *MihomoProcess {
	return &MihomoProcess{
		stopTimeout: defaultStopTimeout,
		startSettle: defaultStartSettle,
	}
}

// timeouts 返回停止超时与启动确认窗口（未设置时返回默认值，兼容零值构造）。
func (p *MihomoProcess) timeouts() (time.Duration, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	stop, settle := p.stopTimeout, p.startSettle
	if stop <= 0 {
		stop = defaultStopTimeout
	}
	if settle <= 0 {
		settle = defaultStartSettle
	}
	return stop, settle
}

// Start 启动 mihomo 内核进程
func (p *MihomoProcess) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("mihomo 已经在运行中")
	}

	binary := findMihomoBinary()
	if binary == "" {
		p.mu.Unlock()
		return fmt.Errorf("未找到 mihomo 可执行文件，请先下载安装")
	}

	cfg := GlobalConfig()
	mihomoDir := filepath.Dir(cfg.MihomoConfigPath)
	// 自动创建 mihomo 工作目录
	if err := os.MkdirAll(mihomoDir, 0700); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("创建 mihomo 工作目录失败: %w", err)
	}
	configPath := cfg.MihomoConfigPath
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		p.mu.Unlock()
		return fmt.Errorf("mihomo 配置文件不存在: %s，请先在订阅页面应用订阅生成配置", configPath)
	}

	output := newCappedBuffer(processOutputLimit)
	cmd := exec.Command(binary, "-d", mihomoDir)
	cmd.Stdout = output
	cmd.Stderr = output
	// 独立进程组：停止时向整个进程组发信号，
	// 避免子进程继承输出管道导致 Wait 在强制终止后仍长期阻塞。
	configureProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("启动 mihomo 失败: %w", err)
	}

	p.cmd = cmd
	p.running = true
	p.pid = cmd.Process.Pid
	p.exited = make(chan error, 1)
	p.output = output
	exited := p.exited
	p.mu.Unlock()

	// goroutine 等待进程退出并更新状态
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.running = false
		p.pid = 0
		p.mu.Unlock()
		exited <- err
		if err != nil {
			Errorf("mihomo 进程退出: %v", err)
		} else {
			Infof("mihomo 进程正常退出")
		}
	}()

	// 等待存活确认窗口：窗口内退出视为启动失败，附带进程输出便于诊断；
	// 窗口结束仍未退出视为启动成功。失败路径立即返回，不做无意义的固定等待。
	_, settle := p.timeouts()
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case err := <-exited:
		detail := strings.TrimSpace(output.String())
		if detail != "" {
			return fmt.Errorf("mihomo 启动后立即退出: %v，进程输出: %s", err, detail)
		}
		return fmt.Errorf("mihomo 启动后立即退出: %v，请检查日志或配置文件", err)
	case <-timer.C:
	}

	Infof("mihomo 已启动: pid=%d, dir=%s", cmd.Process.Pid, mihomoDir)

	// TUN 模式下设置路由修复规则，防止外部无法访问服务器开放端口
	if cfg.System.TUN {
		if err := SetupTUNRouting(); err != nil {
			Warnf("TUN 路由修复设置失败（外部入站连接可能受影响）: %v", err)
		}
	}
	return nil
}

// Stop 停止 mihomo 内核进程。进程未运行时返回 ErrMihomoNotRunning。
func (p *MihomoProcess) Stop() error {
	p.mu.Lock()
	if !p.running || p.cmd == nil || p.cmd.Process == nil {
		p.mu.Unlock()
		return ErrMihomoNotRunning
	}
	proc := p.cmd.Process
	exited := p.exited
	p.mu.Unlock()

	// 发送 SIGTERM（优先整个进程组）
	Infof("正在停止 mihomo: pid=%d", proc.Pid)
	if err := signalProcessTree(proc, sigTerm); err != nil {
		return fmt.Errorf("发送 SIGTERM 失败: %w", err)
	}

	stopTimeout, _ := p.timeouts()
	if exited != nil {
		// 等待进程实际退出（状态变化驱动，而非固定 Sleep）
		select {
		case <-exited:
			Infof("mihomo 已停止")
		case <-time.After(stopTimeout):
			Warnf("mihomo 未在 %v 内退出，强制终止", stopTimeout)
			if err := signalProcessTree(proc, sigKill); err != nil {
				return fmt.Errorf("强制终止 mihomo 失败: %w", err)
			}
			// 等待 Wait 回收（进程组终止后输出管道随之关闭）；
			// 极端情况下回收可能受阻，超时后状态由 Wait goroutine 异步同步。
			select {
			case <-exited:
			case <-time.After(stopTimeout):
				Warnf("等待 mihomo 进程回收超时，退出状态将异步同步")
			}
			Infof("mihomo 已强制终止")
		}
	} else {
		// 兼容手工构造的进程管理器（无退出通知通道）：轮询运行状态
		deadline := time.Now().Add(stopTimeout)
		for {
			p.mu.RLock()
			running := p.running
			p.mu.RUnlock()
			if !running {
				break
			}
			if time.Now().After(deadline) {
				if err := signalProcessTree(proc, sigKill); err != nil {
					return fmt.Errorf("强制终止 mihomo 失败: %w", err)
				}
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// 清理 TUN 路由修复规则，恢复系统网络状态
	if err := RestoreTUNRouting(); err != nil {
		Warnf("TUN 路由规则清理失败: %v", err)
	}
	return nil
}

// Restart 重启 mihomo。
// 未运行时直接启动；停止失败（非 ErrMihomoNotRunning）时取消重启并返回原因，
// 避免掩盖停止失败的根因后继续误启动。
func (p *MihomoProcess) Restart() error {
	if err := p.Stop(); err != nil {
		if errors.Is(err, ErrMihomoNotRunning) {
			Infof("mihomo 未在运行，直接启动")
		} else {
			return fmt.Errorf("停止 mihomo 失败，已取消重启: %w", err)
		}
	}
	return p.Start()
}

// Status 返回运行状态和 PID
func (p *MihomoProcess) Status() (bool, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running, p.pid
}

// IsRunning 检查是否在运行
func (p *MihomoProcess) IsRunning() bool {
	running, _ := p.Status()
	return running
}
