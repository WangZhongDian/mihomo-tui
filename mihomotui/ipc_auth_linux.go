//go:build linux

package mihomotui

import (
	"fmt"
	"net"
	"syscall"
)

func peerCredentialsFromConn(conn net.Conn) (ipcPeerCredentials, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return ipcPeerCredentials{}, fmt.Errorf("IPC 连接不是 Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return ipcPeerCredentials{}, err
	}
	var credential *syscall.Ucred
	var controlErr error
	err = raw.Control(func(fd uintptr) {
		credential, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return ipcPeerCredentials{}, err
	}
	if controlErr != nil {
		return ipcPeerCredentials{}, controlErr
	}
	if credential == nil {
		return ipcPeerCredentials{}, fmt.Errorf("IPC 连接缺少 peer credentials")
	}
	return ipcPeerCredentials{PID: int(credential.Pid), UID: credential.Uid, GID: credential.Gid}, nil
}
