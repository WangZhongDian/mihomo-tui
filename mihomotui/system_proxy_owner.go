package mihomotui

import (
	"os"
	"os/user"
	"strconv"
)

// homeDirOfUser 返回指定用户的主目录；查不到时返回空串。
func homeDirOfUser(name string) string {
	u, err := user.Lookup(name)
	if err != nil || u.HomeDir == "" {
		return ""
	}
	return u.HomeDir
}

// lookupUserIDs 返回指定用户的 uid/gid。
func lookupUserIDs(name string) (uid, gid int, ok bool) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, false
	}
	uidValue, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, false
	}
	gidValue, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, false
	}
	return uidValue, gidValue, true
}

// chownToSystemProxyTarget 在以 root 代写其他用户文件后，把所有者改回目标用户。
// 否则该用户会失去对自己 rc 文件的写权限。非 root 或无法确定目标用户时跳过。
func chownToSystemProxyTarget(path string) {
	uid, gid, ok := systemProxyTargetUID()
	if !ok {
		return
	}
	if err := os.Chown(path, uid, gid); err != nil {
		Warnf("恢复 %s 的所有者失败: %v", path, err)
	}
}
