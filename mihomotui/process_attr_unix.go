//go:build !windows

package mihomotui

import (
	"os"
	"os/exec"
	"syscall"
)

var (
	sigTerm = syscall.SIGTERM
	sigKill = syscall.SIGKILL
)

// configureProcessGroup 让 mihomo 运行在独立进程组中，
// 以便停止时能向整个进程组发送信号，避免子进程遗留。
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessTree 向进程组发送信号；失败时回退到仅向主进程发送。
func signalProcessTree(proc *os.Process, sig os.Signal) error {
	if err := syscall.Kill(-proc.Pid, sig.(syscall.Signal)); err == nil {
		return nil
	}
	return proc.Signal(sig)
}
