//go:build windows

package mihomotui

import (
	"os"
	"os/exec"
)

// Windows 的 os.Process.Signal 仅支持 Kill；退化为直接终止主进程。
var (
	sigTerm = os.Kill
	sigKill = os.Kill
)

func configureProcessGroup(cmd *exec.Cmd) {}

func signalProcessTree(proc *os.Process, sig os.Signal) error {
	return proc.Signal(sig)
}
