package mihomotui

import (
	"context"
	"errors"
	"fmt"
	"mihomotui/mihomotui/i18n"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// 会话注入只覆盖系统代理相关的 8 个变量，避免把无关环境带进用户会话。
// 键名与顺序直接取自 shell 注入块，防止两处定义漂移。
var systemProxyEnvKeys = func() []string {
	pairs := systemProxyEnvPairs(nil)
	keys := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		keys = append(keys, kv[0])
	}
	return keys
}()

// sessionCommandTimeout 单条会话命令的超时，避免 dbus/systemd 不可用时阻塞 UI。
const sessionCommandTimeout = 3 * time.Second

// ApplySystemProxyToSession 把代理环境变量推送到当前桌面/登录会话：
//   - 当前进程（os.Setenv）：让 TUI 自身的订阅下载、版本检查立即走代理
//   - systemd --user 管理器：之后新启动的终端/服务自动继承
//   - D-Bus 激活环境：之后由桌面新启动的图形应用自动继承
//
// 只在 Linux 且存在用户会话总线时执行；失败只记录日志并返回错误，
// 调用方不应因此回滚已经写入 shell 配置文件的注入块。
func ApplySystemProxyToSession(cfg *Config) error {
	vars := systemProxyEnvVars(cfg)
	for key, value := range vars {
		if err := os.Setenv(key, value); err != nil {
			Warnf("设置进程环境变量 %s 失败: %v", key, err)
		}
	}
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return nil
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		Debugf("未检测到用户会话总线，跳过系统代理会话注入")
		return nil
	}

	assignments := make([]string, 0, len(systemProxyEnvKeys))
	for _, key := range systemProxyEnvKeys {
		assignments = append(assignments, key+"="+vars[key])
	}

	var errs []error
	if err := runSessionCommand("systemctl", append([]string{"--user", "set-environment"}, assignments...)...); err != nil {
		errs = append(errs, fmt.Errorf("systemctl --user set-environment 失败: %w", err))
	}
	if err := runSessionCommand("dbus-update-activation-environment", append([]string{"--systemd"}, assignments...)...); err != nil {
		errs = append(errs, fmt.Errorf("dbus-update-activation-environment 失败: %w", err))
	}
	if len(errs) > 0 {
		Warnf("系统代理会话注入部分失败: %v", errors.Join(errs...))
		return errors.Join(errs...)
	}
	Infof("系统代理环境变量已注入当前会话（systemd --user 与 D-Bus 激活环境）")
	return nil
}

// SyncSystemProxySession 按开关状态同步当前会话的环境变量。
func SyncSystemProxySession(enabled bool) error {
	if !enabled {
		return ClearSystemProxyFromSession()
	}
	return ApplySystemProxyToSession(GlobalConfig())
}

// ClearSystemProxyFromSession 从当前进程与 systemd --user 会话中移除代理变量。
// D-Bus 激活环境不支持删除单个变量，只能在下一次 Apply 时覆盖，此处不做处理。
func ClearSystemProxyFromSession() error {
	for _, key := range systemProxyEnvKeys {
		if err := os.Unsetenv(key); err != nil {
			Warnf("清除进程环境变量 %s 失败: %v", key, err)
		}
	}
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return nil
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		return nil
	}
	args := append([]string{"--user", "unset-environment"}, systemProxyEnvKeys...)
	if err := runSessionCommand("systemctl", args...); err != nil {
		Warnf("从 systemd --user 移除系统代理变量失败: %v", err)
		return err
	}
	Infof("已从当前会话移除系统代理环境变量")
	return nil
}

// SystemProxySessionHint 返回开启系统代理后给用户的提示文案。
// 当前终端已经通过 SyncSystemProxySession 注入，但用户已打开的其他 shell
// 只能靠 source 或重新打开——开启时块里有 export，source 是有效的。
func SystemProxySessionHint() string {
	return i18n.T("system_proxy.session_hint")
}

// SystemProxyClearHint 返回关闭系统代理后给用户的提示文案。
//
// 这里不能用 source：注入块已被删除，文件里没有 unset 语句，
// source 只会重新执行剩余内容，旧变量原样留在已运行的 shell 里。
// 必须显式 unset，或开一个不带这些变量的新会话。
func SystemProxyClearHint() string {
	return i18n.T("system_proxy.clear_hint")
}

// systemProxyEnvVars 生成代理环境变量表（与 shell 注入块内容保持一致）。
func systemProxyEnvVars(cfg *Config) map[string]string {
	pairs := systemProxyEnvPairs(cfg)
	vars := make(map[string]string, len(pairs))
	for _, kv := range pairs {
		vars[kv[0]] = kv[1]
	}
	return vars
}

// runSessionCommand 执行会话命令；命令不存在时视为跳过（返回 nil）。
func runSessionCommand(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		Debugf("未找到会话命令 %s，跳过", name)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionCommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return fmt.Errorf("%w（%s）", err, detail)
		}
		return err
	}
	return nil
}
