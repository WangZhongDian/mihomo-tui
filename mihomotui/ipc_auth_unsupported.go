//go:build !linux

package mihomotui

import (
	"fmt"
	"net"
)

func peerCredentialsFromConn(net.Conn) (ipcPeerCredentials, error) {
	return ipcPeerCredentials{}, fmt.Errorf("当前平台不支持 Unix socket peer credentials")
}
