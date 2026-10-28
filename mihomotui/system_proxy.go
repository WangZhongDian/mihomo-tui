package mihomotui

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sysProxyBlockStart = "# >>> mihomo-tui system proxy >>>"
	sysProxyBlockEnd   = "# <<< mihomo-tui system proxy <<<"

	// sysProxyNoProxy 本机回环不走代理，避免把本地服务也塞进代理。
	sysProxyNoProxy = "localhost,127.0.0.1,::1"
)

// SystemProxyPorts 返回系统代理使用的 HTTP（混合）与 SOCKS5 端口。
// 混合端口未配置时回退 HTTP 端口，再回退内置默认值。
func SystemProxyPorts(cfg *Config) (httpPort, socksPort int) {
	if cfg != nil {
		httpPort = cfg.Mihomo.MixedPort
		if httpPort <= 0 {
			httpPort = cfg.Mihomo.HTTPPort
		}
		socksPort = cfg.Mihomo.SOCKS5Port
	}
	if httpPort <= 0 {
		httpPort = DefaultMixedPort
	}
	if socksPort <= 0 {
		socksPort = DefaultSOCKS5Port
	}
	return httpPort, socksPort
}

// systemProxyEnvPairs 按固定顺序返回代理环境变量，保证注入块内容可幂等比对。
func systemProxyEnvPairs(cfg *Config) [][2]string {
	httpPort, socksPort := SystemProxyPorts(cfg)
	httpAddr := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	socksAddr := fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
	return [][2]string{
		{"http_proxy", httpAddr},
		{"https_proxy", httpAddr},
		{"HTTP_PROXY", httpAddr},
		{"HTTPS_PROXY", httpAddr},
		{"ALL_PROXY", socksAddr},
		{"all_proxy", socksAddr},
		{"no_proxy", sysProxyNoProxy},
		{"NO_PROXY", sysProxyNoProxy},
	}
}

// buildSystemProxyBlock 生成写入 shell 配置文件的标记块。
func buildSystemProxyBlock(cfg *Config) string {
	var sb strings.Builder
	sb.WriteString(sysProxyBlockStart)
	sb.WriteByte('\n')
	for _, kv := range systemProxyEnvPairs(cfg) {
		fmt.Fprintf(&sb, "export %s=%s\n", kv[0], kv[1])
	}
	sb.WriteString(sysProxyBlockEnd)
	sb.WriteByte('\n')
	return sb.String()
}

// ProxyPortListening 检查本地代理端口是否在监听。
// 注入代理环境变量后所有走 http_proxy 的程序都会连这个端口，
// 内核没起时注入只会让网络看起来"更坏"，所以开启前必须自检。
func ProxyPortListening(port int) bool {
	if port <= 0 {
		return false
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// SetSystemProxyEnv 开启或关闭系统代理环境变量的持久化注入。
// 开启时把 export 块写入 ~/.bashrc、~/.zshrc、~/.profile（幂等，内容不变则不写）；
// 关闭时清除这些文件中的标记块。会话环境由 SyncSystemProxySession 负责。
func (c *Config) SetSystemProxyEnv(enabled bool) error {
	if c == nil {
		c = GlobalConfig()
	}
	if !enabled {
		if err := c.clearSystemProxyEnv(); err != nil {
			return err
		}
		Infof("系统代理环境变量已清除")
		return nil
	}

	changed, err := upsertSystemProxyBlock(c.shellConfigFiles(), buildSystemProxyBlock(c))
	if err != nil {
		return err
	}
	httpPort, socksPort := SystemProxyPorts(c)
	if changed {
		Infof("系统代理环境变量已注入: http=127.0.0.1:%d socks=127.0.0.1:%d", httpPort, socksPort)
	} else {
		Debugf("系统代理环境变量已是最新，跳过写入")
	}
	return nil
}

// ReconcileSystemProxyEnv 按当前用户的偏好对账 shell 配置文件里的注入块。
// 用于 TUI 启动、端口变更、以及历史上被 `systemctl stop` 误删后的自动恢复：
// 偏好为开启时补齐/更新注入块，为关闭时清掉残留块。内容一致时不产生写入。
func ReconcileSystemProxyEnv() (bool, error) {
	cfg := GlobalConfig()
	if !GetSystemProxyPreference() {
		return clearSystemProxyEnvFrom(shellConfigFiles())
	}
	return upsertSystemProxyBlock(shellConfigFiles(), buildSystemProxyBlock(cfg))
}

// ReconcileSystemProxyEnvIfUsable 是 ReconcileSystemProxyEnv 的安全包装：
// 偏好为开启但代理端口未监听时拒绝注入。
//
// 内核没起时补齐注入会把指向死端口的代理变量写进 rc 文件，新开终端直接断网——
// 这正是"停内核后网络突然不通"的成因。
//
// 返回值：
//   - changed: 是否修改了文件
//   - skipped: 是否因端口不可用而跳过（偏好为开启但端口未监听）
//   - err:     文件读写错误
func ReconcileSystemProxyEnvIfUsable() (changed bool, skipped bool, err error) {
	if GetSystemProxyPreference() {
		httpPort, _ := SystemProxyPorts(GlobalConfig())
		if !ProxyPortListening(httpPort) {
			Warnf("系统代理偏好为开启，但 127.0.0.1:%d 未监听，跳过注入以避免断网", httpPort)
			return false, true, nil
		}
	}
	changed, err = ReconcileSystemProxyEnv()
	return changed, false, err
}

// DisableSystemProxyOnKernelStop 在停止 mihomo 内核时同步关闭系统代理。
//
// 内核停掉后代理端口不再监听，若继续保留注入，新开的终端会拿到指向死端口的
// 代理变量，表现为"网络突然不通"。因此显式停内核时把注入一并撤掉。
// 返回是否真的执行了关闭（原本已是关闭状态时返回 false）。
//
// 注意执行顺序：先把偏好落盘为关闭，再清理文件。这样即使清理文件失败，
// TUI 启动时的 ReconcileSystemProxyEnv 也不会把死端口的注入补回来。
func DisableSystemProxyOnKernelStop() (bool, error) {
	if !GetSystemProxyPreference() {
		return false, nil
	}
	if err := SetSystemProxyPreference(false); err != nil {
		return false, fmt.Errorf("关闭系统代理偏好失败: %w", err)
	}
	cfg := GlobalConfig()
	clearErr := cfg.SetSystemProxyEnv(false)
	// 会话清理尽力而为：已打开的终端无法被外部修改，只能清当前进程与用户会话。
	if serr := SyncSystemProxySession(false); serr != nil {
		Warnf("清理系统代理会话变量失败: %v", serr)
	}
	if clearErr != nil {
		return true, clearErr
	}
	Infof("停止内核：已自动关闭系统代理")
	return true, nil
}

func (c *Config) clearSystemProxyEnv() error {
	_, err := clearSystemProxyEnvFrom(c.shellConfigFiles())
	return err
}

func (c *Config) shellConfigFiles() []string {
	return shellConfigFiles()
}

// shellConfigFiles 返回需要注入的 shell 配置文件。
//
// 注意 bash 的读取顺序：登录 shell 优先读 ~/.bash_profile → ~/.bash_login → ~/.profile，
// 只有三者都不存在时才读 ~/.profile。因此只要用户建过 .bash_profile 或 .bash_login，
// 往 .profile 写入就是死代码——SSH 登录服务器时看不到代理变量的典型原因。
// 这里把已存在的优先文件一并纳入，.bashrc 仍然覆盖交互式非登录 shell。
func shellConfigFiles() []string {
	home := systemProxyTargetHome()
	if home == "" {
		return nil
	}
	files := []string{filepath.Join(home, ".bashrc")}
	// 登录 shell 的实际入口：存在则必须写，否则 .profile 不会被读取。
	for _, name := range []string{".bash_profile", ".bash_login"} {
		p := filepath.Join(home, name)
		if _, statErr := os.Stat(p); statErr == nil {
			files = append(files, p)
		}
	}
	return append(files,
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	)
}

// systemProxyTargetHome 返回系统代理注入应该写入哪个用户的主目录。
//
// `sudo mihomo-tui` 是服务器上的常见用法（连接 root daemon）。此时 os.UserHomeDir()
// 返回 /root，注入块会写进 /root/.bashrc，而真正登录的普通用户根本读不到。
// 因此 root + SUDO_USER 场景改为写入发起 sudo 的用户主目录。
func systemProxyTargetHome() string {
	if os.Geteuid() == 0 {
		if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
			if home := homeDirOfUser(sudoUser); home != "" {
				return home
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// systemProxyTargetUID 返回注入目标用户的 uid/gid；无法确定时返回 false。
// root 写入其他用户主目录后必须把文件所有者改回去，否则该用户无法再编辑自己的 rc 文件。
func systemProxyTargetUID() (uid, gid int, ok bool) {
	if os.Geteuid() != 0 {
		return 0, 0, false
	}
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "" || sudoUser == "root" {
		return 0, 0, false
	}
	return lookupUserIDs(sudoUser)
}

// upsertSystemProxyBlock 把标记块写入所有 shell 配置文件，返回是否有文件被修改。
// 已存在的文件原地更新；若三个文件都不存在（极少数环境），则创建 ~/.profile
// 作为落点，避免"偏好已开启但哪里都没注入"的静默失效。
func upsertSystemProxyBlock(files []string, block string) (bool, error) {
	changed := false
	existing := 0
	for _, f := range files {
		modified, err := upsertBlockInFile(f, sysProxyBlockStart, sysProxyBlockEnd, block)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return changed, fmt.Errorf("写入 %s 失败: %w", f, err)
		}
		existing++
		if modified {
			// root 代写普通用户的 rc 文件后，把所有者改回去。
			chownToSystemProxyTarget(f)
		}
		changed = changed || modified
	}
	if existing > 0 {
		return changed, nil
	}
	// 没有任何 shell 配置文件：创建 ~/.profile 承载注入。
	home := systemProxyTargetHome()
	if home == "" {
		return false, fmt.Errorf("无法确定用户主目录，系统代理环境变量未写入")
	}
	path := filepath.Join(home, ".profile")
	if err := os.WriteFile(path, []byte(block), 0644); err != nil {
		return false, fmt.Errorf("创建 %s 失败: %w", path, err)
	}
	chownToSystemProxyTarget(path)
	return true, nil
}

// clearSystemProxyEnvFrom 移除所有 shell 配置文件中的标记块，返回是否有文件被修改。
func clearSystemProxyEnvFrom(files []string) (bool, error) {
	changed := false
	for _, f := range files {
		modified, err := removeBlockFromFile(f, sysProxyBlockStart, sysProxyBlockEnd)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return changed, fmt.Errorf("清理 %s 失败: %w", f, err)
		}
		changed = changed || modified
	}
	return changed, nil
}

// CleanupSystemProxyEnv 清理所有用户 shell 配置文件中的标记块。
// 仅在卸载和显式 `mihomo-tui cleanup` 时调用；`systemctl stop` 不会走这里。
// 各用户的 system_proxy 偏好保持不变：TUI 启动时的 ReconcileSystemProxyEnv
// 会按偏好重新补齐或清理，因此清理与偏好之间的暂时不一致可以自愈。
func CleanupSystemProxyEnv() error {
	var allHomes []string

	// 1. root 用户
	allHomes = append(allHomes, "/root")

	// 2. /home/* 下所有普通用户
	if entries, err := os.ReadDir("/home"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			allHomes = append(allHomes, filepath.Join("/home", entry.Name()))
		}
	}

	// 3. 当前用户（兜底，兼容非 root 直接运行 cleanup 或 home 不在 /home 的场景）
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/root" && !strings.HasPrefix(home, "/home/") {
		allHomes = append(allHomes, home)
	}

	for _, home := range allHomes {
		for _, f := range shellConfigFilesInHome(home) {
			if _, err := removeBlockFromFile(f, sysProxyBlockStart, sysProxyBlockEnd); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				Warnf("清理 %s 失败: %v", f, err)
			}
		}
	}
	return nil
}

// SystemProxyTargetHomeForDisplay 返回系统代理注入的目标主目录（供诊断输出）。
func SystemProxyTargetHomeForDisplay() string {
	if home := systemProxyTargetHome(); home != "" {
		return home
	}
	return "(未确定)"
}

// shellConfigFilesInHome 返回指定主目录下所有可能承载注入的文件。
// 清理时必须覆盖全部候选（含 .bash_profile/.bash_login），
// 否则换过登录入口的文件会残留旧块。
func shellConfigFilesInHome(home string) []string {
	return []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".bash_login"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".profile"),
	}
}

// stripSystemProxyBlock 从内容中移除标记块（纯字符串处理）。
// 未找到标记块时原样返回；起始标记缺少结束标记时同样原样返回，
// 避免把标记之后的用户内容误删。
func stripSystemProxyBlock(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false
	removed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock && trimmed == sysProxyBlockStart {
			inBlock = true
			removed = true
			// 块前若留下空行，一并吞掉，避免清理后出现连续空行。
			if n := len(out); n > 0 && out[n-1] == "" {
				out = out[:n-1]
			}
			continue
		}
		if inBlock {
			if trimmed == sysProxyBlockEnd {
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}

	// 起始标记没有配对结束标记：文件被手工改坏，不动它。
	if !removed || inBlock {
		return content
	}
	return strings.Join(out, "\n")
}

// removeBlockFromFile 删除文件中的标记块，返回是否发生修改。
func removeBlockFromFile(path, startMarker, endMarker string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(data)
	cleaned := stripSystemProxyBlock(content)
	if cleaned == content {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(cleaned), fileModeOrDefault(path, 0644))
}

// upsertBlockInFile 把标记块写入文件：已有同内容块则完全不动文件（保持 mtime），
// 否则原地替换旧块或重新插入。返回是否修改了文件。
func upsertBlockInFile(path, startMarker, endMarker, block string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(data)
	if strings.Contains(content, block) {
		return false, nil
	}

	base := stripSystemProxyBlock(content)
	if base == content && strings.Contains(content, startMarker) {
		// 起始标记缺少结束标记（历史文件被改坏）：从标记处截断，
		// 否则会在坏块之后追加出第二个块。
		if idx := strings.Index(content, startMarker); idx >= 0 {
			base = strings.TrimRight(content[:idx], "\n")
			if base != "" {
				base += "\n"
			}
		}
	}

	// .bashrc 里通常有「非交互式直接 return」的守卫（Ubuntu/Debian 默认就有）：
	// 追加到文件末尾的块在 `ssh host 'cmd'` 这类非交互 shell 中根本执行不到。
	// 这种情况必须把块插到守卫之前。
	if insertAt := bashrcNonInteractiveReturnIndex(base); insertAt >= 0 {
		head := base[:insertAt]
		tail := base[insertAt:]
		if head != "" && !strings.HasSuffix(head, "\n") {
			head += "\n"
		}
		return true, os.WriteFile(path, []byte(head+block+tail), fileModeOrDefault(path, 0644))
	}

	if base != "" && !strings.HasSuffix(base, "\n") {
		base += "\n"
	}
	return true, os.WriteFile(path, []byte(base+block), fileModeOrDefault(path, 0644))
}

// bashrcNonInteractiveReturnIndex 查找 .bashrc 中「非交互式 shell 直接 return」
// 守卫的位置，返回该守卫的起始字节偏移；没有此类守卫时返回 -1。
//
// 识别形如：
//
//	case $- in
//	    *i*) ;;
//	      *) return;;
//	esac
//
// 的写法（Ubuntu/Debian 默认）。块插在它之前，才能让非交互式 SSH 命令
// 也拿到代理变量。
func bashrcNonInteractiveReturnIndex(content string) int {
	lines := strings.SplitAfter(content, "\n")
	offset := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "case $- in" || trimmed == "case \"$-\" in" {
			// 从 case 开始向下找匹配的 esac，中间出现 return 就认为命中。
			// 守卫写法通常是 `*) return;;`，return 不在行首，因此用包含判断。
			hasReturn := false
			for _, probe := range lines[i+1:] {
				probeTrimmed := strings.TrimSpace(probe)
				if probeTrimmed == "esac" {
					break
				}
				if containsReturnStatement(probeTrimmed) {
					hasReturn = true
					break
				}
			}
			if hasReturn {
				return offset
			}
		}
		offset += len(line)
	}
	return -1
}

// containsReturnStatement 判断一行 shell 代码是否为 return 语句。
// 兼容 `return`、`*) return;;`、`;; *) return 0 ;;` 等写法，
// 但排除 `return_val=1` 这类标识符。
func containsReturnStatement(line string) bool {
	idx := strings.Index(line, "return")
	for idx >= 0 {
		// 前一个字符必须是分隔符，避免把 myreturn 之类当成语句。
		prevOK := idx == 0 || !isShellIdentChar(rune(line[idx-1]))
		if prevOK {
			rest := line[idx+len("return"):]
			// 后一个字符必须是分隔符/空白/结束，避免 return_code 之类。
			if rest == "" || !isShellIdentChar(rune(rest[0])) {
				return true
			}
		}
		next := strings.Index(line[idx+len("return"):], "return")
		if next < 0 {
			break
		}
		idx += len("return") + next
	}
	return false
}

func isShellIdentChar(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// fileModeOrDefault 返回文件现有权限；stat 失败时使用 fallback。
func fileModeOrDefault(path string, fallback os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return fallback
}
