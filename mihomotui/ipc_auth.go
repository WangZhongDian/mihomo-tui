package mihomotui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
)

const (
	// ipcAccessGroup 的成员只能调用不含凭据的状态查询 API。
	ipcAccessGroup = "mihomo-tui"
	// ipcOperatorGroup 的成员可管理订阅和规则订阅，但不能执行 root 系统操作。
	ipcOperatorGroup = "mihomo-tui-operator"
)

type ipcRole int

const (
	ipcRoleDenied ipcRole = iota
	ipcRoleReadOnly
	ipcRoleOperator
	ipcRoleAdmin
)

type ipcPeerCredentials struct {
	PID int
	UID uint32
	GID uint32
}

type ipcAuthContextKey struct{}
type ipcRoleContextKey struct{}

type ipcAuthorizer struct {
	runsAsRoot  bool
	ownerUID    uint32
	accessGID   uint32
	operatorGID uint32
	hasGroups   bool
}

func newIPCAuthorizer() (*ipcAuthorizer, error) {
	auth := &ipcAuthorizer{ownerUID: uint32(os.Geteuid()), runsAsRoot: os.Geteuid() == 0}
	if !auth.runsAsRoot {
		return auth, nil
	}

	accessGID, err := lookupIPCGroupGID(ipcAccessGroup)
	if err != nil {
		return nil, err
	}
	operatorGID, err := lookupIPCGroupGID(ipcOperatorGroup)
	if err != nil {
		return nil, err
	}
	auth.accessGID = accessGID
	auth.operatorGID = operatorGID
	auth.hasGroups = true
	return auth, nil
}

func lookupIPCGroupGID(name string) (uint32, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("未找到 IPC 授权组 %q；请先通过 install_service 安装服务，或创建该组后再启动 root 守护进程: %w", name, err)
	}
	gid, err := strconv.ParseUint(group.Gid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("解析 IPC 授权组 %q 的 GID 失败: %w", name, err)
	}
	return uint32(gid), nil
}

func (a *ipcAuthorizer) configureSocketDirectory(dir string) error {
	if a.runsAsRoot {
		if !a.hasGroups {
			return fmt.Errorf("root 守护进程缺少 IPC 授权组")
		}
		if err := os.Chown(dir, 0, int(a.accessGID)); err != nil {
			return fmt.Errorf("设置 socket 目录属组失败: %w", err)
		}
		if err := os.Chmod(dir, 0750); err != nil {
			return fmt.Errorf("设置 socket 目录权限失败: %w", err)
		}
		return nil
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("设置 socket 目录权限失败: %w", err)
	}
	return nil
}

func (a *ipcAuthorizer) configureSocketPermissions(sock string) error {
	if a.runsAsRoot {
		if err := os.Chown(sock, 0, int(a.accessGID)); err != nil {
			return fmt.Errorf("设置 socket 属组失败: %w", err)
		}
		if err := os.Chmod(sock, 0660); err != nil {
			return fmt.Errorf("设置 socket 权限失败: %w", err)
		}
		return nil
	}
	if err := os.Chmod(sock, 0600); err != nil {
		return fmt.Errorf("设置 socket 权限失败: %w", err)
	}
	return nil
}

func (a *ipcAuthorizer) connContext(ctx context.Context, conn net.Conn) context.Context {
	peer, err := peerCredentialsFromConn(conn)
	if err != nil {
		// 非 root daemon 的 socket 是当前用户私有的 0600 文件；在不支持
		// SO_PEERCRED 的平台上可依赖该内核权限边界继续工作。root daemon
		// 则必须取得 peer credentials，失败时默认拒绝请求。
		if !a.runsAsRoot {
			return context.WithValue(ctx, ipcAuthContextKey{}, ipcPeerCredentials{UID: a.ownerUID})
		}
		Warnf("无法读取 IPC 调用方凭据: %v", err)
		return ctx
	}
	return context.WithValue(ctx, ipcAuthContextKey{}, peer)
}

func (a *ipcAuthorizer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := a.roleForRequest(r)
		r = r.WithContext(context.WithValue(r.Context(), ipcRoleContextKey{}, role))
		if role == ipcRoleDenied {
			Warnf("拒绝未授权 IPC 请求: method=%s path=%s", r.Method, r.URL.Path)
			writeError(w, http.StatusForbidden, fmt.Errorf("无权访问 IPC 服务；请加入 %s 或 %s 组", ipcAccessGroup, ipcOperatorGroup))
			return
		}
		if role == ipcRoleReadOnly && !isIPCReadOnlyRequest(r) {
			Warnf("拒绝只读 IPC 请求: method=%s path=%s", r.Method, r.URL.Path)
			writeError(w, http.StatusForbidden, fmt.Errorf("当前 IPC 身份仅允许读取不含敏感信息的状态"))
			return
		}
		if role == ipcRoleOperator && isIPCRootOnlyRequest(r) {
			Warnf("拒绝非 root IPC 特权请求: method=%s path=%s", r.Method, r.URL.Path)
			writeError(w, http.StatusForbidden, fmt.Errorf("该操作需要 root 权限"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *ipcAuthorizer) roleForRequest(r *http.Request) ipcRole {
	peer, ok := r.Context().Value(ipcAuthContextKey{}).(ipcPeerCredentials)
	if !ok {
		return ipcRoleDenied
	}
	if peer.UID == 0 || peer.UID == a.ownerUID {
		return ipcRoleAdmin
	}
	if !a.runsAsRoot || !a.isGroupMember(peer, a.accessGID) {
		return ipcRoleDenied
	}
	if a.isGroupMember(peer, a.operatorGID) {
		return ipcRoleOperator
	}
	return ipcRoleReadOnly
}

func (a *ipcAuthorizer) isGroupMember(peer ipcPeerCredentials, expectedGID uint32) bool {
	if !a.hasGroups {
		return false
	}
	if peer.GID == expectedGID {
		return true
	}
	account, err := user.LookupId(strconv.FormatUint(uint64(peer.UID), 10))
	if err != nil {
		Warnf("无法查询 IPC 调用用户 UID=%d: %v", peer.UID, err)
		return false
	}
	groups, err := account.GroupIds()
	if err != nil {
		Warnf("无法查询 IPC 调用用户组 UID=%d: %v", peer.UID, err)
		return false
	}
	wanted := strconv.FormatUint(uint64(expectedGID), 10)
	for _, gid := range groups {
		if gid == wanted {
			return true
		}
	}
	return false
}

func isIPCReadOnlyRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	// 只读组只可查看不含订阅 URL、配置路径或凭据的运行状态。
	// 配置、订阅列表、规则订阅和 API 凭据都必须由 operator/root 身份访问。
	switch path.Clean(r.URL.Path) {
	case "/api/v1/ping", "/api/v1/daemon/info", "/api/v1/mihomo/status", "/api/v1/mihomo/version", "/api/v1/mihomo/latest-version", "/api/v1/mihomo/upgrade/progress":
		return true
	default:
		return false
	}
}

func isIPCRootOnlyRequest(r *http.Request) bool {
	cleanPath := path.Clean(r.URL.Path)
	if cleanPath == "/api/v1/config" && r.Method != http.MethodGet {
		return true
	}
	if strings.HasPrefix(cleanPath, "/api/v1/mihomo/start") ||
		strings.HasPrefix(cleanPath, "/api/v1/mihomo/stop") ||
		strings.HasPrefix(cleanPath, "/api/v1/mihomo/restart") ||
		strings.HasPrefix(cleanPath, "/api/v1/mihomo/upgrade") {
		return true
	}
	return cleanPath == "/api/v1/daemon/shutdown" || cleanPath == "/api/v1/mihomo/external-resources/download"
}

func requestIPCRole(r *http.Request) ipcRole {
	if role, ok := r.Context().Value(ipcRoleContextKey{}).(ipcRole); ok {
		return role
	}
	return ipcRoleDenied
}
