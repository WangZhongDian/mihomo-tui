//go:build linux

package mihomotui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	tunRoutingTable     = "2022"
	tunPrivateRulePref  = "10010"
	tunMarkRulePref     = "10020"
	tunConnectionMark   = "0x100"
	tunIPTablesTable    = "mangle"
	tunRuleComment      = "mihomo-tui"
	tunPreroutingChain  = "MIHOMO_TUI_PREROUTING"
	tunOutputChain      = "MIHOMO_TUI_OUTPUT"
	tunForwardChain     = "MIHOMO_TUI_FORWARD"
	tunRoutingStateFile = "tun-routing-state.json"
)

type tunRoutingState struct {
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
}

type tunCommand struct {
	Name string
	Args []string
}

func (c tunCommand) String() string {
	return c.Name + " " + strings.Join(c.Args, " ")
}

// getDefaultGateway 解析系统默认路由，返回物理网卡名和网关 IP。
func getDefaultGateway() (iface, gateway string, err error) {
	out, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("获取默认路由失败: %w, output: %s", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		var candidateInterface, candidateGateway string
		for i := 0; i < len(parts)-1; i++ {
			switch parts[i] {
			case "dev":
				candidateInterface = parts[i+1]
			case "via":
				candidateGateway = parts[i+1]
			}
		}
		if candidateInterface != "" && candidateGateway != "" {
			return candidateInterface, candidateGateway, nil
		}
	}
	return "", "", fmt.Errorf("无法从路由表解析默认网关: %s", strings.TrimSpace(string(out)))
}

func tunRoutingStatePath() string {
	return filepath.Join(GetConfigDir(), tunRoutingStateFile)
}

func loadTUNRoutingState() (tunRoutingState, error) {
	data, err := os.ReadFile(tunRoutingStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return tunRoutingState{}, nil
		}
		return tunRoutingState{}, fmt.Errorf("读取 TUN 路由状态失败: %w", err)
	}
	var state tunRoutingState
	if err := json.Unmarshal(data, &state); err != nil {
		return tunRoutingState{}, fmt.Errorf("解析 TUN 路由状态失败: %w", err)
	}
	return state, nil
}

func saveTUNRoutingState(state tunRoutingState) error {
	path := tunRoutingStatePath()
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化 TUN 路由状态失败: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("写入 TUN 路由状态失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("保存 TUN 路由状态失败: %w", err)
	}
	return nil
}

func runTUNCommand(command tunCommand) (string, error) {
	out, err := exec.Command(command.Name, command.Args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("执行失败: %s: %w; output: %s", command.String(), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func isTUNNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no such file", "no such process", "no chain/target/match", "bad rule", "does a matching rule exist",
		// iptables-nft 在跳转目标链已被删除时使用此措辞。
		// 这表示清理目标已不存在，而不是清理失败。
		"does not exist",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func runTUNCommandIgnoringNotFound(command tunCommand) error {
	_, err := runTUNCommand(command)
	if err == nil {
		return nil
	}
	// iproute2 和 iptables 对不存在规则返回非零；这些情况在清理时可安全忽略，
	// 其余错误必须上报，避免权限或命令故障被静默吞掉。
	if isTUNNotFoundError(err) {
		Debugf("TUN 清理目标不存在: %s", command.String())
		return nil
	}
	return err
}

func tunPolicyRuleAddCommands() []tunCommand {
	return []tunCommand{
		{Name: "ip", Args: []string{"rule", "add", "to", "10.0.0.0/8", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "add", "to", "172.16.0.0/12", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "add", "to", "192.168.0.0/16", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "add", "fwmark", tunConnectionMark, "table", tunRoutingTable, "pref", tunMarkRulePref}},
	}
}

// tunPolicyCleanupCommands 只有在本项目状态文件存在且完整时才会生成策略路由删除命令。
// 这避免在没有 mihomo-tui 所有权证据时按固定 pref/mark 删除其他服务的 ip rule。
func tunPolicyCleanupCommands(state tunRoutingState) ([]tunCommand, error) {
	if state.Interface == "" && state.Gateway == "" {
		return nil, nil
	}
	if state.Interface == "" || state.Gateway == "" {
		return nil, fmt.Errorf("TUN 路由状态不完整，拒绝删除策略路由")
	}
	commands := []tunCommand{
		{Name: "ip", Args: []string{"rule", "del", "to", "10.0.0.0/8", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "del", "to", "172.16.0.0/12", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "del", "to", "192.168.0.0/16", "table", "main", "pref", tunPrivateRulePref}},
		{Name: "ip", Args: []string{"rule", "del", "fwmark", tunConnectionMark, "table", tunRoutingTable, "pref", tunMarkRulePref}},
		{Name: "ip", Args: []string{"route", "del", "default", "via", state.Gateway, "dev", state.Interface, "table", tunRoutingTable}},
	}
	return commands, nil
}

func tunMainJumpCommands() []tunCommand {
	return []tunCommand{
		{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-A", "PREROUTING", "-m", "comment", "--comment", tunRuleComment, "-j", tunPreroutingChain}},
		{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-A", "OUTPUT", "-m", "comment", "--comment", tunRuleComment, "-j", tunOutputChain}},
		{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-A", "FORWARD", "-m", "comment", "--comment", tunRuleComment, "-j", tunForwardChain}},
	}
}

func tunChainNames() []string {
	return []string{tunPreroutingChain, tunOutputChain, tunForwardChain}
}

func tunChainRuleCommands(iface string) map[string][]tunCommand {
	comment := []string{"-m", "comment", "--comment", tunRuleComment}
	return map[string][]tunCommand{
		tunPreroutingChain: {
			{Name: "iptables", Args: append([]string{"-t", tunIPTablesTable, "-A", tunPreroutingChain, "-i", iface, "-m", "conntrack", "--ctstate", "NEW"}, append(comment, "-j", "CONNMARK", "--set-mark", tunConnectionMark)...)},
			{Name: "iptables", Args: append([]string{"-t", tunIPTablesTable, "-A", tunPreroutingChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark")...)},
		},
		tunOutputChain: {
			{Name: "iptables", Args: append([]string{"-t", tunIPTablesTable, "-A", tunOutputChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark")...)},
		},
		tunForwardChain: {
			{Name: "iptables", Args: append([]string{"-t", tunIPTablesTable, "-A", tunForwardChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark")...)},
		},
	}
}

func ensureTUNChain(chain string) error {
	if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-S", chain}}); err == nil {
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-F", chain}}); err != nil {
			return err
		}
		return nil
	} else if !isTUNNotFoundError(err) {
		return fmt.Errorf("检查 iptables 链 %s 失败: %w", chain, err)
	}
	_, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-N", chain}})
	return err
}

func ensureTUNMainJump(command tunCommand) error {
	check := command
	check.Args = append([]string{}, command.Args...)
	for i, arg := range check.Args {
		if arg == "-A" {
			check.Args[i] = "-C"
			break
		}
	}
	if _, err := runTUNCommand(check); err == nil {
		return nil
	}
	_, err := runTUNCommand(command)
	return err
}

func cleanupTUNIPTables() error {
	var errs []error
	for _, command := range tunMainJumpCommands() {
		deleteCommand := command
		deleteCommand.Args = append([]string{}, command.Args...)
		for i, arg := range deleteCommand.Args {
			if arg == "-A" {
				deleteCommand.Args[i] = "-D"
				break
			}
		}
		// 只删除本项目带明确 comment、跳转到本项目专用链的规则。
		for {
			_, err := runTUNCommand(deleteCommand)
			if err == nil {
				continue
			}
			if !isTUNNotFoundError(err) {
				errs = append(errs, err)
			}
			break
		}
	}
	for _, chain := range tunChainNames() {
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-S", chain}}); err != nil {
			if !isTUNNotFoundError(err) {
				errs = append(errs, fmt.Errorf("检查 mihomo-tui 链 %s 失败: %w", chain, err))
			}
			continue
		}
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-F", chain}}); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-X", chain}}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cleanupTUNRouting() error {
	Infof("[cleanupTUNRouting] 正在清理 mihomo-tui TUN 路由规则...")
	var errs []error

	// iptables 专用链和带 comment 的 jump 是可独立识别的；即使状态文件丢失，
	// 也可以安全清理。但策略路由没有 comment，必须以持久化状态作为所有权凭据。
	state, stateErr := loadTUNRoutingState()
	if stateErr != nil {
		errs = append(errs, stateErr)
	} else if commands, err := tunPolicyCleanupCommands(state); err != nil {
		errs = append(errs, err)
	} else if len(commands) > 0 {
		policyClean := true
		for _, command := range commands {
			if err := runTUNCommandIgnoringNotFound(command); err != nil {
				policyClean = false
				errs = append(errs, err)
			}
		}
		// 删除失败时保留状态文件，以便下一次 cleanup 仍能精确重试，
		// 而不是丢失所有权信息后误删固定 pref/mark 的其他规则。
		if policyClean {
			if err := os.Remove(tunRoutingStatePath()); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("删除 TUN 路由状态失败: %w", err))
			}
		}
	}

	if err := cleanupTUNIPTables(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func rollbackTUNSetup(setupErr error) error {
	if cleanupErr := cleanupTUNRouting(); cleanupErr != nil {
		return errors.Join(setupErr, fmt.Errorf("自动回滚失败；请执行 mihomo-tui tun_diagnose 并手动清理: %w", cleanupErr))
	}
	return setupErr
}

// SetupTUNRouting 设置 TUN 模式下的路由修复规则，解决外部无法访问服务器开放端口的问题。
// 所有 iptables 规则都位于 mihomo-tui 专用链，并带有 comment；清理时绝不会扫描或删除其他 CONNMARK 规则。
func SetupTUNRouting() error {
	iface, gateway, err := getDefaultGateway()
	if err != nil {
		return fmt.Errorf("获取默认网关失败: %w", err)
	}
	Infof("[SetupTUNRouting] 默认网关: iface=%s gateway=%s", iface, gateway)

	if err := cleanupTUNRouting(); err != nil {
		return fmt.Errorf("清理旧 mihomo-tui TUN 规则失败；请先执行 mihomo-tui tun_diagnose 并修复后重试: %w", err)
	}

	if _, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"route", "add", "default", "via", gateway, "dev", iface, "table", tunRoutingTable}}); err != nil {
		return fmt.Errorf("设置 mihomo-tui 策略路由失败: %w", err)
	}
	if err := saveTUNRoutingState(tunRoutingState{Interface: iface, Gateway: gateway}); err != nil {
		setupErr := fmt.Errorf("保存 mihomo-tui TUN 路由状态失败: %w", err)
		if cleanupErr := runTUNCommandIgnoringNotFound(tunCommand{Name: "ip", Args: []string{"route", "del", "default", "via", gateway, "dev", iface, "table", tunRoutingTable}}); cleanupErr != nil {
			return errors.Join(setupErr, fmt.Errorf("自动回滚默认路由失败；请执行 mihomo-tui tun_diagnose 并手动清理: %w", cleanupErr))
		}
		return setupErr
	}

	for _, command := range tunPolicyRuleAddCommands() {
		if _, err := runTUNCommand(command); err != nil {
			return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui 策略规则失败: %w", err))
		}
	}

	for _, chain := range tunChainNames() {
		if err := ensureTUNChain(chain); err != nil {
			return rollbackTUNSetup(fmt.Errorf("创建 mihomo-tui iptables 链失败: %w", err))
		}
	}
	for _, command := range tunMainJumpCommands() {
		if err := ensureTUNMainJump(command); err != nil {
			return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui iptables 跳转规则失败: %w", err))
		}
	}
	for _, commands := range tunChainRuleCommands(iface) {
		for _, command := range commands {
			if _, err := runTUNCommand(command); err != nil {
				return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui CONNMARK 规则失败: %w", err))
			}
		}
	}

	Infof("[SetupTUNRouting] TUN 路由修复已设置: iface=%s gateway=%s table=%s", iface, gateway, tunRoutingTable)
	return nil
}

// DescribeTUNRouting 返回当前默认路由以及 SetupTUNRouting 将执行的命令。
// 它不会修改路由或 iptables，可用于部署前诊断和 dry-run 审核。
func DescribeTUNRouting() ([]string, error) {
	iface, gateway, err := getDefaultGateway()
	if err != nil {
		return nil, err
	}
	commands := []tunCommand{{Name: "ip", Args: []string{"route", "add", "default", "via", gateway, "dev", iface, "table", tunRoutingTable}}}
	commands = append(commands, tunPolicyRuleAddCommands()...)
	for _, chain := range tunChainNames() {
		commands = append(commands,
			tunCommand{Name: "iptables", Args: []string{"-t", tunIPTablesTable, "-N", chain}},
		)
	}
	commands = append(commands, tunMainJumpCommands()...)
	for _, chainCommands := range tunChainRuleCommands(iface) {
		commands = append(commands, chainCommands...)
	}
	result := make([]string, 0, len(commands)+1)
	result = append(result, fmt.Sprintf("默认路由: iface=%s gateway=%s", iface, gateway))
	for _, command := range commands {
		result = append(result, command.String())
	}
	return result, nil
}

// RestoreTUNRouting 清理 mihomo-tui 自己创建的 TUN 路由修复规则，将系统网络恢复到 TUN 开启前的状态。
func RestoreTUNRouting() error {
	if err := cleanupTUNRouting(); err != nil {
		return fmt.Errorf("清理 mihomo-tui TUN 路由规则失败: %w", err)
	}
	Infof("[RestoreTUNRouting] mihomo-tui TUN 路由规则已清理")
	return nil
}

// CleanupEnvironment 停止/卸载时统一清理系统代理环境变量和 TUN 路由规则。
func CleanupEnvironment() {
	Infof("[CleanupEnvironment] 开始清理环境...")

	if err := CleanupSystemProxyEnv(); err != nil {
		Warnf("清理系统代理环境变量失败: %v", err)
	} else {
		Infof("[CleanupEnvironment] 系统代理环境变量已清理")
	}

	if err := RestoreTUNRouting(); err != nil {
		Warnf("清理 TUN 路由规则失败: %v", err)
	} else {
		Infof("[CleanupEnvironment] TUN 路由规则已清理")
	}
}
