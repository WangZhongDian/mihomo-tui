package mihomotui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// ErrIPCPermissionDenied 表示 root IPC socket 存在，但当前登录会话没有访问权限。
var ErrIPCPermissionDenied = errors.New("IPC 服务权限不足")

// rootSocketPath 是 root/systemd daemon 的共享 socket；其目录与文件权限由 IPC 授权器收紧。
func rootSocketPath() string {
	return filepath.Join(SocketDir, SocketFile)
}

// userSocketPath 是普通用户 standalone daemon 的私有 socket，避免与 root daemon 争用 /run 路径。
func userSocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), "mihomo-tui-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(base, SocketFile)
}

// daemonSocketPath 根据 daemon 实际运行身份选择 socket。root daemon 使用共享路径，普通用户 daemon 使用私有路径。
func daemonSocketPath() string {
	if os.Geteuid() == 0 {
		return rootSocketPath()
	}
	return userSocketPath()
}

// clientSocketPath 优先连接当前用户实际可访问的 root daemon；root socket 不存在或已失效时
// 回退到当前用户的一体模式 socket。无权限访问 root socket 不视为“服务不存在”，
// 由 clientSocketPathWithError 保留并返回权限错误。
func clientSocketPath() string {
	sock, _ := clientSocketPathWithError()
	return sock
}

func clientSocketPathWithError() (string, error) {
	rootPath := rootSocketPath()
	if err := dialSocket(rootPath); err == nil {
		return rootPath, nil
	} else if isSocketPermissionError(err) {
		return rootPath, ipcPermissionError(rootPath, err)
	}
	return userSocketPath(), nil
}

// SocketPath 返回当前客户端会尝试连接的 socket 路径。
func SocketPath() string {
	return clientSocketPath()
}

func canDialSocket(sock string) bool {
	return dialSocket(sock) == nil
}

func dialSocket(sock string) error {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func isSocketPermissionError(err error) bool {
	return os.IsPermission(err) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

func ipcPermissionError(sock string, err error) error {
	userName := os.Getenv("USER")
	if userName == "" {
		userName = "当前用户"
	}
	return fmt.Errorf("%w：检测到 root IPC 服务 %s，但用户 %s 的当前登录会话无权访问（%v）。请执行 sudo mihomo-tui grant_operator %s，然后注销桌面会话并重新登录；临时生效可执行 newgrp mihomo-tui 后再启动 TUI", ErrIPCPermissionDenied, sock, userName, err, userName)
}

// IsIPCPermissionError 判断错误是否为 root IPC 服务访问权限不足。
func IsIPCPermissionError(err error) bool {
	return errors.Is(err, ErrIPCPermissionDenied)
}
