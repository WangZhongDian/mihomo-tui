package mihomotui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// MihomoProcess mihomo 内核进程管理器
type MihomoProcess struct {
	mu      sync.RWMutex
	cmd     *exec.Cmd
	running bool
	pid     int
}

// NewMihomoProcess 创建进程管理器
func NewMihomoProcess() *MihomoProcess {
	return &MihomoProcess{}
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
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("创建 mihomo 工作目录失败: %w", err)
	}
	configPath := cfg.MihomoConfigPath
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		p.mu.Unlock()
		return fmt.Errorf("mihomo 配置文件不存在: %s，请先在订阅页面应用订阅生成配置", configPath)
	}

	p.cmd = exec.Command(binary, "-d", mihomoDir)

	if err := p.cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("启动 mihomo 失败: %w", err)
	}

	p.running = true
	p.pid = p.cmd.Process.Pid
	p.mu.Unlock()

	// goroutine 等待进程退出
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		p.running = false
		p.pid = 0
		p.mu.Unlock()
		if err != nil {
			Errorf("mihomo 进程退出: %v", err)
		} else {
			Infof("mihomo 进程正常退出")
		}
	}()

	// 等待一小段时间确认进程存活（避免启动后立即退出的情况）
	time.Sleep(300 * time.Millisecond)
	p.mu.RLock()
	stillRunning := p.running
	p.mu.RUnlock()
	if !stillRunning {
		return fmt.Errorf("mihomo 启动失败，进程已退出，请检查日志或配置文件")
	}

	Infof("mihomo 已启动: pid=%d, dir=%s", p.pid, mihomoDir)

	// TUN 模式下设置路由修复规则，防止外部无法访问服务器开放端口
	if cfg.System.TUN {
		if err := SetupTUNRouting(); err != nil {
			Warnf("TUN 路由修复设置失败（外部入站连接可能受影响）: %v", err)
		}
	}
	return nil
}

// Stop 停止 mihomo 内核进程
func (p *MihomoProcess) Stop() error {
	p.mu.Lock()
	if !p.running || p.cmd == nil || p.cmd.Process == nil {
		p.mu.Unlock()
		return fmt.Errorf("mihomo 未在运行")
	}
	proc := p.cmd.Process
	p.mu.Unlock()

	// 发送 SIGTERM
	Infof("正在停止 mihomo: pid=%d", proc.Pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("发送 SIGTERM 失败: %w", err)
	}

	// 等待最多 5 秒
	done := make(chan struct{})
	go func() {
		for {
			p.mu.RLock()
			running := p.running
			p.mu.RUnlock()
			if !running {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		Infof("mihomo 已停止")
	case <-time.After(5 * time.Second):
		// 超时，强制 SIGKILL
		Warnf("mihomo 未在 5 秒内退出，强制终止")
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("强制终止 mihomo 失败: %w", err)
		}
		Infof("mihomo 已强制终止")
	}

	// 清理 TUN 路由修复规则，恢复系统网络状态
	if err := RestoreTUNRouting(); err != nil {
		Warnf("TUN 路由规则清理失败: %v", err)
	}
	return nil
}

// Restart 重启 mihomo
func (p *MihomoProcess) Restart() error {
	_ = p.Stop()
	time.Sleep(500 * time.Millisecond)
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
