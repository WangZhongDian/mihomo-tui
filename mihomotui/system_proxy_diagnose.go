package mihomotui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemProxyDiagnostic 描述系统代理注入的逐项检查结果。
type SystemProxyDiagnostic struct {
	PreferenceEnabled bool
	ConfigDir         string
	HTTPPort          int
	SocksPort         int
	PortListening     bool
	ShellFiles        []ShellFileStatus
	SessionVars       []EnvVarStatus
	Notes             []string
	// UnitExecStop 是已安装 systemd unit 的 ExecStop 行（仅 Linux，读不到时为空）。
	UnitExecStop string
}

// ShellFileStatus 单个 shell 配置文件的注入状态。
type ShellFileStatus struct {
	Path    string
	Exists  bool
	HasBlk  bool
	HasHTTP bool
	// LoginEntry 为 true 表示该文件是 bash 登录 shell 的实际入口
	// （.bash_profile / .bash_login / 无前两者时的 .profile）。
	LoginEntry bool
}

// EnvVarStatus 单个环境变量在当前进程中的状态。
type EnvVarStatus struct {
	Name  string
	Value string
	Set   bool
}

// DiagnoseSystemProxy 收集系统代理注入的现状，供 `mihomo-tui doctor` 输出。
// 只读：不修改任何文件或环境变量。
func DiagnoseSystemProxy() SystemProxyDiagnostic {
	cfg := GlobalConfig()
	httpPort, socksPort := SystemProxyPorts(cfg)
	diag := SystemProxyDiagnostic{
		PreferenceEnabled: GetSystemProxyPreference(),
		ConfigDir:         GetConfigDir(),
		HTTPPort:          httpPort,
		SocksPort:         socksPort,
		PortListening:     ProxyPortListening(httpPort),
		UnitExecStop:      installedUnitExecStop(),
	}

	home, _ := os.UserHomeDir()
	// 判定 bash 登录 shell 的真实入口文件。
	loginEntry := ""
	if home != "" {
		switch {
		case fileExists(filepath.Join(home, ".bash_profile")):
			loginEntry = filepath.Join(home, ".bash_profile")
		case fileExists(filepath.Join(home, ".bash_login")):
			loginEntry = filepath.Join(home, ".bash_login")
		default:
			loginEntry = filepath.Join(home, ".profile")
		}
	}

	files := shellConfigFiles()
	if home != "" {
		// 诊断要覆盖全部候选文件，即使未参与注入也要显示状态。
		for _, name := range []string{".bashrc", ".bash_profile", ".bash_login", ".zshrc", ".profile"} {
			p := filepath.Join(home, name)
			if !containsString(files, p) && fileExists(p) {
				files = append(files, p)
			}
		}
	}
	for _, f := range files {
		status := ShellFileStatus{Path: f, Exists: fileExists(f), LoginEntry: f == loginEntry}
		if status.Exists {
			if data, err := os.ReadFile(f); err == nil {
				content := string(data)
				status.HasBlk = strings.Contains(content, sysProxyBlockStart)
				status.HasHTTP = strings.Contains(content, fmt.Sprintf("http://127.0.0.1:%d", httpPort))
			}
		}
		diag.ShellFiles = append(diag.ShellFiles, status)
	}

	for _, name := range systemProxyEnvKeys {
		value, ok := os.LookupEnv(name)
		diag.SessionVars = append(diag.SessionVars, EnvVarStatus{Name: name, Value: value, Set: ok})
	}

	// 结论性提示：直接指出最可能的原因，避免用户自己猜。
	if diag.PreferenceEnabled && !diag.PortListening {
		diag.Notes = append(diag.Notes, fmt.Sprintf(
			"偏好为开启，但 127.0.0.1:%d 未监听：此时走代理的程序会连接失败，请先启动 mihomo 内核。", httpPort))
	}
	if diag.PreferenceEnabled && !anyBlock(diag.ShellFiles) {
		diag.Notes = append(diag.Notes, "偏好为开启，但所有 shell 配置文件都没有注入块：启动一次 TUI 会自动对账补齐。")
	}
	if diag.PreferenceEnabled && anyBlock(diag.ShellFiles) && !loginEntryHasBlock(diag.ShellFiles) {
		diag.Notes = append(diag.Notes, fmt.Sprintf(
			"登录 shell 入口 %s 没有注入块：SSH 登录后不会加载代理变量。", loginEntry))
	}
	if os.Geteuid() == 0 {
		diag.Notes = append(diag.Notes, "当前以 root 运行：会话注入（systemctl --user / dbus）被跳过，只写 shell 配置文件。")
	}
	// 最容易被忽略的一条：只换了二进制、没重装服务时，unit 里仍是旧版
	// `ExecStop=... cleanup`，于是每次 systemctl stop/restart 都会把注入块删掉。
	if strings.Contains(diag.UnitExecStop, "cleanup") && !strings.Contains(diag.UnitExecStop, "--tun-only") {
		diag.Notes = append(diag.Notes, "已安装的 systemd unit 仍是旧版 ExecStop（cleanup 无 --tun-only）："+
			"每次 systemctl stop/restart 都会清掉系统代理注入。请重新执行 sudo mihomo-tui install_service。")
	}
	return diag
}

// installedUnitExecStop 读取已安装 systemd unit 的 ExecStop 行；读不到时返回空串。
func installedUnitExecStop() string {
	data, err := os.ReadFile("/etc/systemd/system/mihomo-tui.service")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ExecStop=") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func anyBlock(files []ShellFileStatus) bool {
	for _, f := range files {
		if f.HasBlk {
			return true
		}
	}
	return false
}

func loginEntryHasBlock(files []ShellFileStatus) bool {
	for _, f := range files {
		if f.LoginEntry {
			return f.HasBlk
		}
	}
	return false
}
