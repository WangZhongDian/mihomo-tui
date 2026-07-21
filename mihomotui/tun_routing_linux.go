//go:build linux

package mihomotui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// 必须早于 mihomo auto-route 的策略规则，并避开 mihomo 默认使用的 table 2022。
	tunRoutingTable          = "100"
	tunPrivateRulePref       = "100"
	tunMarkRulePref          = "200"
	legacyTUNRoutingTable    = "2022"
	legacyTUNPrivateRulePref = "10010"
	legacyTUNMarkRulePref    = "10020"
	tunConnectionMark        = "0x100"
	tunConnectionMask        = "0x100"
	tunRoutingStateFile      = "tun-routing-state.json"
	tunRoutingLockFile       = "/run/mihomo-tui/tun-routing.lock"
	tunRoutingStateVersion   = 2
	tunRoutingStatePreparing = "preparing"
	tunRoutingStateActive    = "active"

	tunFirewallBackendNFT            = "nft"
	tunFirewallBackendIPTablesLegacy = "iptables-legacy"
	tunNFTFamily                     = "ip"
	tunNFTTable                      = "mihomo_tui"
	tunNFTPreroutingChain            = "prerouting"
	tunNFTOutputChain                = "output"
	tunNFTForwardChain               = "forward"

	legacyTUNIPTablesTable = "mangle"
	tunRuleComment         = "mihomo-tui"
	tunPreroutingChain     = "MIHOMO_TUI_PREROUTING"
	tunOutputChain         = "MIHOMO_TUI_OUTPUT"
	tunForwardChain        = "MIHOMO_TUI_FORWARD"
)

var (
	tunDebugSessionMu sync.Mutex
	tunDebugWriterMu  sync.RWMutex
	tunDebugWriter    io.Writer
	tunExecutorMu     sync.RWMutex
	tunExecutor       tunCommandExecutor = hostTUNCommandExecutor{}
)

type tunCommandExecutor interface {
	Run(tunCommand, string) (string, error)
}

type hostTUNCommandExecutor struct{}

func (hostTUNCommandExecutor) Run(command tunCommand, input string) (string, error) {
	cmd := exec.Command(command.Name, command.Args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// setTUNCommandExecutorForTest injects a deterministic command executor into
// the controller. It is deliberately package-private so production code can
// only use the real system executor.
func setTUNCommandExecutorForTest(executor tunCommandExecutor) func() {
	tunExecutorMu.Lock()
	previous := tunExecutor
	tunExecutor = executor
	tunExecutorMu.Unlock()
	return func() {
		tunExecutorMu.Lock()
		tunExecutor = previous
		tunExecutorMu.Unlock()
	}
}

type tunRoutingState struct {
	Version         int    `json:"version,omitempty"`
	Phase           string `json:"phase,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	Interface       string `json:"interface"`
	Gateway         string `json:"gateway"`
	RoutingTable    string `json:"routing_table,omitempty"`
	PrivateRulePref string `json:"private_rule_pref,omitempty"`
	MarkRulePref    string `json:"mark_rule_pref,omitempty"`
	FirewallBackend string `json:"firewall_backend,omitempty"`
	ConnectionMark  string `json:"connection_mark,omitempty"`
	ConnectionMask  string `json:"connection_mask,omitempty"`
	NFTTable        string `json:"nft_table,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
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
	out, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"route", "show", "default"}})
	if err != nil {
		return "", "", fmt.Errorf("获取默认路由失败: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
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
	return "", "", fmt.Errorf("无法从路由表解析默认网关: %s", strings.TrimSpace(out))
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

func newTUNRoutingState(iface, gateway, backend string) (tunRoutingState, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return tunRoutingState{}, fmt.Errorf("生成 TUN 路由安装标识失败: %w", err)
	}
	return tunRoutingState{
		Version:         tunRoutingStateVersion,
		Phase:           tunRoutingStatePreparing,
		InstanceID:      hex.EncodeToString(random),
		Interface:       iface,
		Gateway:         gateway,
		RoutingTable:    tunRoutingTable,
		PrivateRulePref: tunPrivateRulePref,
		MarkRulePref:    tunMarkRulePref,
		FirewallBackend: backend,
		ConnectionMark:  tunConnectionMark,
		ConnectionMask:  tunConnectionMask,
		NFTTable:        tunNFTTable,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s tunRoutingState) isOwnedCurrentNFTTable() bool {
	return s.FirewallBackend == tunFirewallBackendNFT && (s.NFTTable == "" || s.NFTTable == tunNFTTable)
}

func tunRuleCommentForState(state tunRoutingState) string {
	if state.InstanceID == "" {
		return tunRuleComment
	}
	return tunRuleComment + ":" + state.InstanceID
}

func tunRoutingLockPath() string {
	return tunRoutingLockFile
}

func withTUNRoutingLock(fn func() error) error {
	path := tunRoutingLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建 TUN 路由锁目录失败: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("打开 TUN 路由锁失败: %w", err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("获取 TUN 路由锁失败: %w", err)
	}
	defer func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func setTUNDebugWriter(w io.Writer) func() {
	tunDebugWriterMu.Lock()
	previous := tunDebugWriter
	tunDebugWriter = w
	tunDebugWriterMu.Unlock()
	return func() {
		tunDebugWriterMu.Lock()
		tunDebugWriter = previous
		tunDebugWriterMu.Unlock()
	}
}

func writeTUNDebugf(format string, args ...any) {
	tunDebugWriterMu.RLock()
	w := tunDebugWriter
	tunDebugWriterMu.RUnlock()
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "[tun-debug] "+format+"\n", args...)
}

func suppressTUNDebugOutput(command tunCommand) bool {
	joined := strings.Join(command.Args, " ")
	if command.Name == "nft" && strings.Contains(joined, "-j") && strings.Contains(joined, "list ruleset") {
		return true
	}
	// ip -j link/route dumps are diagnostic input for the controller, not a
	// useful stdout trace. Keep the command and byte count while reporting the
	// parsed conflict/default-route summary separately.
	return command.Name == "ip" && (strings.HasPrefix(joined, "-j ") || strings.Contains(joined, " -j "))
}

func runTUNCommand(command tunCommand) (string, error) {
	return runTUNCommandInput(command, "")
}

func runTUNCommandInput(command tunCommand, input string) (string, error) {
	writeTUNDebugf("$ %s", command.String())
	if input != "" {
		writeTUNDebugf("stdin (%d bytes):", len(input))
		for _, line := range strings.Split(strings.TrimSuffix(input, "\n"), "\n") {
			writeTUNDebugf("| %s", line)
		}
	}
	tunExecutorMu.RLock()
	executor := tunExecutor
	tunExecutorMu.RUnlock()
	out, err := executor.Run(command, input)
	output := strings.TrimSpace(out)
	if output != "" {
		if suppressTUNDebugOutput(command) {
			writeTUNDebugf("output: <structured diagnostic output omitted, %d bytes>", len(out))
		} else {
			for _, line := range strings.Split(output, "\n") {
				writeTUNDebugf("output: %s", line)
			}
		}
	}
	if err != nil {
		writeTUNDebugf("command failed: %v", err)
		return out, fmt.Errorf("执行失败: %s: %w; output: %s", command.String(), err, output)
	}
	writeTUNDebugf("command succeeded")
	return out, nil
}

func isTUNNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no such file", "no such process", "no chain/target/match", "bad rule", "does a matching rule exist", "does not exist",
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
	if isTUNNotFoundError(err) {
		Debugf("TUN 清理目标不存在: %s", command.String())
		return nil
	}
	return err
}

func isTUNRouteTableMissingError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "fib table does not exist") || strings.Contains(message, "routing table does not exist")
}

func ensureTUNDefaultRoute(iface, gateway string) error {
	show := tunCommand{Name: "ip", Args: []string{"route", "show", "table", tunRoutingTable}}
	out, err := runTUNCommand(show)
	if err != nil && !isTUNRouteTableMissingError(err) {
		return fmt.Errorf("检查 mihomo-tui 策略路由表失败: %w", err)
	}
	if err != nil {
		out = ""
	}
	expected := "default via " + gateway + " dev " + iface
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "default") {
			if strings.Contains(line, expected) {
				return nil
			}
			return fmt.Errorf("策略路由表 %s 已有其他默认路由: %s", tunRoutingTable, line)
		}
	}
	_, err = runTUNCommand(tunCommand{Name: "ip", Args: []string{"route", "add", "default", "via", gateway, "dev", iface, "table", tunRoutingTable}})
	return err
}

func runTUNRuleAddIgnoringExists(command tunCommand) error {
	_, err := runTUNCommand(command)
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "file exists") {
		return nil
	}
	return err
}

func tunPolicyRuleCommands(table, privatePref, markPref, action string) []tunCommand {
	return []tunCommand{
		{Name: "ip", Args: []string{"rule", action, "to", "10.0.0.0/8", "table", "main", "pref", privatePref}},
		{Name: "ip", Args: []string{"rule", action, "to", "172.16.0.0/12", "table", "main", "pref", privatePref}},
		{Name: "ip", Args: []string{"rule", action, "to", "192.168.0.0/16", "table", "main", "pref", privatePref}},
		{Name: "ip", Args: []string{"rule", action, "to", "100.64.0.0/10", "table", "main", "pref", privatePref}},
		{Name: "ip", Args: []string{"rule", action, "fwmark", tunConnectionMark + "/" + tunConnectionMask, "table", table, "pref", markPref}},
	}
}

func tunPolicyRuleAddCommands() []tunCommand {
	return tunPolicyRuleCommands(tunRoutingTable, tunPrivateRulePref, tunMarkRulePref, "add")
}

// routingValues 识别 a0fa5d2 写入但缺少路由参数的旧状态文件。
func (s tunRoutingState) routingValues() (table, privatePref, markPref string) {
	if s.RoutingTable == "" && s.PrivateRulePref == "" && s.MarkRulePref == "" {
		return legacyTUNRoutingTable, legacyTUNPrivateRulePref, legacyTUNMarkRulePref
	}
	return s.RoutingTable, s.PrivateRulePref, s.MarkRulePref
}

func tunPolicyCleanupCommands(state tunRoutingState) ([]tunCommand, error) {
	if state.Interface == "" && state.Gateway == "" {
		return nil, nil
	}
	if state.Interface == "" || state.Gateway == "" {
		return nil, fmt.Errorf("TUN 路由状态不完整，拒绝删除策略路由")
	}
	table, privatePref, markPref := state.routingValues()
	if table == "" || privatePref == "" || markPref == "" {
		return nil, fmt.Errorf("TUN 路由状态缺少路由表或优先级，拒绝删除策略路由")
	}
	var commands []tunCommand
	if table == legacyTUNRoutingTable && privatePref == legacyTUNPrivateRulePref && markPref == legacyTUNMarkRulePref {
		// a0fa5d2 的状态文件使用 2022/10010/10020，且没有 CGNAT 例外和 mask。
		commands = []tunCommand{
			{Name: "ip", Args: []string{"rule", "del", "to", "10.0.0.0/8", "table", "main", "pref", privatePref}},
			{Name: "ip", Args: []string{"rule", "del", "to", "172.16.0.0/12", "table", "main", "pref", privatePref}},
			{Name: "ip", Args: []string{"rule", "del", "to", "192.168.0.0/16", "table", "main", "pref", privatePref}},
			{Name: "ip", Args: []string{"rule", "del", "fwmark", tunConnectionMark, "table", table, "pref", markPref}},
		}
	} else {
		commands = tunPolicyRuleCommands(table, privatePref, markPref, "del")
	}
	commands = append(commands, tunCommand{Name: "ip", Args: []string{"route", "del", "default", "via", state.Gateway, "dev", state.Interface, "table", table}})
	return commands, nil
}

func selectTUNFirewallBackend(nftAvailable, iptablesAvailable bool, iptablesVersion string) (string, error) {
	if nftAvailable {
		return tunFirewallBackendNFT, nil
	}
	if !iptablesAvailable {
		return "", fmt.Errorf("未找到 nft；同时未找到 iptables-legacy，无法设置 TUN 回包规则")
	}
	version := strings.ToLower(iptablesVersion)
	if strings.Contains(version, "nf_tables") || strings.Contains(version, "iptables-nft") {
		return "", fmt.Errorf("当前系统使用 iptables-nft，但未找到 nft 命令；请安装 nftables")
	}
	return tunFirewallBackendIPTablesLegacy, nil
}

func detectTUNFirewallBackend() (string, error) {
	if _, err := exec.LookPath("nft"); err == nil {
		return selectTUNFirewallBackend(true, false, "")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return selectTUNFirewallBackend(false, false, "")
	}
	out, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"--version"}})
	if err != nil {
		return "", fmt.Errorf("检测 iptables 后端失败: %w", err)
	}
	return selectTUNFirewallBackend(false, true, out)
}

func validateTUNInterfaceName(iface string) error {
	if iface == "" || len(iface) > 15 {
		return fmt.Errorf("无效的网络接口名称 %q", iface)
	}
	for _, r := range iface {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '.', ':', '@':
			continue
		default:
			return fmt.Errorf("网络接口名称包含不安全字符: %q", iface)
		}
	}
	return nil
}

type tunLinkInfo struct {
	InfoKind string `json:"info_kind"`
}

type tunLink struct {
	IfName   string      `json:"ifname"`
	LinkInfo tunLinkInfo `json:"linkinfo"`
}

type tunRoute struct {
	Dst string `json:"dst"`
	Dev string `json:"dev"`
}

type tunPreflight struct {
	Backend      string
	Interface    string
	Gateway      string
	ExternalTUNs []string
	IPv6Warning  bool
}

func parseActiveTUNInterfaces(linkJSON, routeJSON []byte) ([]string, error) {
	var links []tunLink
	if err := json.Unmarshal(linkJSON, &links); err != nil {
		return nil, fmt.Errorf("解析网络接口 JSON 失败: %w", err)
	}
	var routes []tunRoute
	if err := json.Unmarshal(routeJSON, &routes); err != nil {
		return nil, fmt.Errorf("解析 IPv4 路由 JSON 失败: %w", err)
	}
	defaultDevices := make(map[string]struct{})
	for _, route := range routes {
		if route.Dst == "default" && route.Dev != "" {
			defaultDevices[route.Dev] = struct{}{}
		}
	}
	var active []string
	for _, link := range links {
		if link.LinkInfo.InfoKind != "tun" || link.IfName == "" {
			continue
		}
		if _, ok := defaultDevices[link.IfName]; ok {
			active = append(active, link.IfName)
		}
	}
	return active, nil
}

func detectExternalActiveTUNs() ([]string, error) {
	links, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"-j", "-d", "link", "show"}})
	if err != nil {
		return nil, fmt.Errorf("读取网络接口失败: %w", err)
	}
	routes, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"-j", "-4", "route", "show", "table", "all"}})
	if err != nil {
		return nil, fmt.Errorf("读取 IPv4 路由失败: %w", err)
	}
	return parseActiveTUNInterfaces([]byte(links), []byte(routes))
}

func hasIPv6DefaultRoute() (bool, error) {
	out, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"-6", "route", "show", "default"}})
	if err != nil {
		return false, fmt.Errorf("读取 IPv6 默认路由失败: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

func collectTUNPreflight(backend string) (tunPreflight, error) {
	iface, gateway, err := getDefaultGateway()
	if err != nil {
		return tunPreflight{}, err
	}
	if err := validateTUNInterfaceName(iface); err != nil {
		return tunPreflight{}, err
	}
	externalTUNs, err := detectExternalActiveTUNs()
	if err != nil {
		return tunPreflight{}, err
	}
	ipv6Warning, err := hasIPv6DefaultRoute()
	if err != nil {
		return tunPreflight{}, err
	}
	return tunPreflight{Backend: backend, Interface: iface, Gateway: gateway, ExternalTUNs: externalTUNs, IPv6Warning: ipv6Warning}, nil
}

func (p tunPreflight) validateForApply() error {
	if len(p.ExternalTUNs) > 0 {
		return fmt.Errorf("检测到其他活跃 TUN 默认路由（%s）；为避免与 Clash Verge、mihomo 或 sing-box 冲突，拒绝并发启用", strings.Join(p.ExternalTUNs, ", "))
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("TUN 路由修复需要 root 权限")
	}
	return nil
}

func findTUNRulePriorityCollisions(rules string) []string {
	var collisions []string
	for _, line := range strings.Split(rules, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, tunPrivateRulePref+":") || strings.HasPrefix(line, tunMarkRulePref+":") {
			collisions = append(collisions, line)
		}
	}
	return collisions
}

func validateTUNRouteSlotsAreFree() error {
	out, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"-4", "route", "show", "table", tunRoutingTable}})
	if err != nil {
		if !isTUNRouteTableMissingError(err) {
			return fmt.Errorf("检查策略路由表 %s 失败: %w", tunRoutingTable, err)
		}
		// iproute2 在从未创建过的 table 上以非零状态输出
		// “FIB table does not exist”。这表示空表，而不是被占用；
		// 清空输出，避免把诊断文本误判为路由内容。
		out = ""
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("策略路由表 %s 已被其他规则占用: %s", tunRoutingTable, strings.TrimSpace(out))
	}
	rules, err := runTUNCommand(tunCommand{Name: "ip", Args: []string{"-4", "rule", "show"}})
	if err != nil {
		return fmt.Errorf("检查策略路由优先级失败: %w", err)
	}
	collisions := findTUNRulePriorityCollisions(rules)
	if len(collisions) > 0 {
		return fmt.Errorf("策略路由优先级 %s/%s 已被其他规则占用: %s", tunPrivateRulePref, tunMarkRulePref, strings.Join(collisions, "; "))
	}
	return nil
}

func buildTUNNFTScript(iface string) (string, error) {
	return buildTUNNFTScriptWithComment(iface, tunRuleComment)
}

// buildTUNNFTScriptWithComment creates an isolated IPv4 nftables table.  The
// project bit is ORed into conntrack/packet marks, never replacing marks owned
// by VPNs, policy routers, or another firewall manager.
func buildTUNNFTScriptWithComment(iface, comment string) (string, error) {
	if err := validateTUNInterfaceName(iface); err != nil {
		return "", err
	}
	if comment == "" {
		return "", fmt.Errorf("TUN 防火墙规则注释不能为空")
	}
	quotedIface := strconv.Quote(iface)
	quotedComment := strconv.Quote(comment)
	restoreMark := "ct state established,related ct mark & " + tunConnectionMask + " != 0 meta mark set meta mark | " + tunConnectionMask + " comment " + quotedComment
	lines := []string{
		"add table " + tunNFTFamily + " " + tunNFTTable,
		"add chain " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTPreroutingChain + " { type filter hook prerouting priority filter; policy accept; }",
		"add chain " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTOutputChain + " { type route hook output priority filter; policy accept; }",
		"add chain " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTForwardChain + " { type filter hook forward priority filter; policy accept; }",
		"add rule " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTPreroutingChain + " iifname " + quotedIface + " ct state new ct mark set ct mark | " + tunConnectionMask + " comment " + quotedComment,
		"add rule " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTPreroutingChain + " " + restoreMark,
		"add rule " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTOutputChain + " " + restoreMark,
		"add rule " + tunNFTFamily + " " + tunNFTTable + " " + tunNFTForwardChain + " " + restoreMark,
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func setupTUNNFTFirewall(iface string, state tunRoutingState) error {
	script, err := buildTUNNFTScriptWithComment(iface, tunRuleCommentForState(state))
	if err != nil {
		return err
	}
	if _, err := runTUNCommandInput(tunCommand{Name: "nft", Args: []string{"-f", "-"}}, script); err != nil {
		return fmt.Errorf("创建原生 nftables TUN 规则失败: %w", err)
	}
	return nil
}

func cleanupTUNNFTFirewall(state tunRoutingState) error {
	// New state files carry an unpredictable per-install marker. Verify it
	// before deleting the fixed project table name so a coincidental table name
	// cannot be removed by this process. Older state files lack the marker and
	// remain supported as an explicit migration path.
	if state.InstanceID != "" {
		out, err := runTUNCommand(tunCommand{Name: "nft", Args: []string{"list", "table", tunNFTFamily, tunNFTTable}})
		if err != nil {
			if isTUNNotFoundError(err) {
				return nil
			}
			return fmt.Errorf("验证 mihomo-tui nftables 表所有权失败: %w", err)
		}
		if !strings.Contains(out, strconv.Quote(tunRuleCommentForState(state))) {
			return fmt.Errorf("拒绝删除 %s 表：未找到当前安装标识 %q", tunNFTTable, tunRuleCommentForState(state))
		}
	}
	command := tunCommand{Name: "nft", Args: []string{"delete", "table", tunNFTFamily, tunNFTTable}}
	if err := runTUNCommandIgnoringNotFound(command); err != nil {
		return fmt.Errorf("删除 mihomo-tui nftables 表失败: %w", err)
	}
	return nil
}

type nftChainLocation struct {
	Family string
	Table  string
	Chain  string
}

type nftRuleReference struct {
	nftChainLocation
	Handle uint64
	Target string
}

type nftJSONEnvelope struct {
	NFTables []map[string]json.RawMessage `json:"nftables"`
}

type nftJSONChain struct {
	Family string `json:"family"`
	Table  string `json:"table"`
	Name   string `json:"name"`
}

type nftJSONRule struct {
	Family string            `json:"family"`
	Table  string            `json:"table"`
	Chain  string            `json:"chain"`
	Handle uint64            `json:"handle"`
	Expr   []json.RawMessage `json:"expr"`
}

func isTUNProjectChain(chain string) bool {
	switch chain {
	case tunPreroutingChain, tunOutputChain, tunForwardChain:
		return true
	default:
		return false
	}
}

func findTUNProjectTarget(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "jump" || key == "goto" {
				if targetObject, ok := child.(map[string]any); ok {
					if target, ok := targetObject["target"].(string); ok && isTUNProjectChain(target) {
						return target
					}
				}
			}
			if target := findTUNProjectTarget(child); target != "" {
				return target
			}
		}
	case []any:
		for _, child := range typed {
			if target := findTUNProjectTarget(child); target != "" {
				return target
			}
		}
	}
	return ""
}

func parseTUNNFTablesJSON(data []byte) (chains []nftChainLocation, rules []nftRuleReference, err error) {
	var envelope nftJSONEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil, fmt.Errorf("解析 nftables JSON 失败: %w", err)
	}
	for _, object := range envelope.NFTables {
		if raw, ok := object["chain"]; ok {
			var chain nftJSONChain
			if err := json.Unmarshal(raw, &chain); err != nil {
				return nil, nil, fmt.Errorf("解析 nftables chain 失败: %w", err)
			}
			if isTUNProjectChain(chain.Name) {
				chains = append(chains, nftChainLocation{Family: chain.Family, Table: chain.Table, Chain: chain.Name})
			}
		}
		if raw, ok := object["rule"]; ok {
			var rule nftJSONRule
			if err := json.Unmarshal(raw, &rule); err != nil {
				return nil, nil, fmt.Errorf("解析 nftables rule 失败: %w", err)
			}
			for _, expression := range rule.Expr {
				var value any
				if err := json.Unmarshal(expression, &value); err != nil {
					return nil, nil, fmt.Errorf("解析 nftables rule expression 失败: %w", err)
				}
				if target := findTUNProjectTarget(value); target != "" && rule.Handle > 0 {
					rules = append(rules, nftRuleReference{
						nftChainLocation: nftChainLocation{Family: rule.Family, Table: rule.Table, Chain: rule.Chain},
						Handle:           rule.Handle,
						Target:           target,
					})
					break
				}
			}
		}
	}
	return chains, rules, nil
}

func listLegacyTUNNFTArtifacts() ([]nftChainLocation, []nftRuleReference, error) {
	out, err := runTUNCommand(tunCommand{Name: "nft", Args: []string{"-j", "-a", "list", "ruleset"}})
	if err != nil {
		return nil, nil, fmt.Errorf("读取 nftables JSON ruleset 失败: %w", err)
	}
	return parseTUNNFTablesJSON([]byte(out))
}

func nftArtifactKey(location nftChainLocation) string {
	return location.Family + "\x00" + location.Table + "\x00" + location.Chain
}

func cleanupLegacyTUNArtifactsWithNFT() error {
	chains, rules, err := listLegacyTUNNFTArtifacts()
	if err != nil {
		return err
	}
	if len(chains) == 0 && len(rules) == 0 {
		Debugf("nftables 中未发现真实的旧 MIHOMO_TUI_* 链；忽略 iptables-nft 幽灵链兼容提示")
		return nil
	}

	var errs []error
	seenRules := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		key := nftArtifactKey(rule.nftChainLocation) + "\x00" + strconv.FormatUint(rule.Handle, 10)
		if _, exists := seenRules[key]; exists {
			continue
		}
		seenRules[key] = struct{}{}
		command := tunCommand{Name: "nft", Args: []string{"delete", "rule", rule.Family, rule.Table, rule.Chain, "handle", strconv.FormatUint(rule.Handle, 10)}}
		if err := runTUNCommandIgnoringNotFound(command); err != nil {
			errs = append(errs, fmt.Errorf("删除旧 nftables 跳转 %s/%s/%s handle=%d target=%s 失败: %w", rule.Family, rule.Table, rule.Chain, rule.Handle, rule.Target, err))
		}
	}

	seenChains := make(map[string]struct{}, len(chains))
	for _, chain := range chains {
		key := nftArtifactKey(chain)
		if _, exists := seenChains[key]; exists {
			continue
		}
		seenChains[key] = struct{}{}
		if err := runTUNCommandIgnoringNotFound(tunCommand{Name: "nft", Args: []string{"flush", "chain", chain.Family, chain.Table, chain.Chain}}); err != nil {
			errs = append(errs, fmt.Errorf("清空旧 nftables 项目链 %s/%s/%s 失败: %w", chain.Family, chain.Table, chain.Chain, err))
			continue
		}
		if err := runTUNCommandIgnoringNotFound(tunCommand{Name: "nft", Args: []string{"delete", "chain", chain.Family, chain.Table, chain.Chain}}); err != nil {
			errs = append(errs, fmt.Errorf("删除旧 nftables 项目链 %s/%s/%s 失败: %w", chain.Family, chain.Table, chain.Chain, err))
		}
	}
	if cleanupErr := errors.Join(errs...); cleanupErr != nil {
		return cleanupErr
	}

	remainingChains, remainingRules, err := listLegacyTUNNFTArtifacts()
	if err != nil {
		return fmt.Errorf("验证旧 nftables 规则迁移失败: %w", err)
	}
	if len(remainingChains) > 0 || len(remainingRules) > 0 {
		return fmt.Errorf("旧 nftables 规则迁移后仍有残留: chains=%d references=%d", len(remainingChains), len(remainingRules))
	}
	Infof("已迁移旧 mihomo-tui nftables 规则: chains=%d references=%d", len(chains), len(rules))
	return nil
}

func tunMainJumpCommands() []tunCommand {
	return []tunCommand{
		{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-A", "PREROUTING", "-m", "comment", "--comment", tunRuleComment, "-j", tunPreroutingChain}},
		{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-A", "OUTPUT", "-m", "comment", "--comment", tunRuleComment, "-j", tunOutputChain}},
		{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-A", "FORWARD", "-m", "comment", "--comment", tunRuleComment, "-j", tunForwardChain}},
	}
}

func tunChainNames() []string {
	return []string{tunPreroutingChain, tunOutputChain, tunForwardChain}
}

func tunChainRuleCommands(iface string) map[string][]tunCommand {
	comment := []string{"-m", "comment", "--comment", tunRuleComment}
	return map[string][]tunCommand{
		tunPreroutingChain: {
			{Name: "iptables", Args: append([]string{"-t", legacyTUNIPTablesTable, "-A", tunPreroutingChain, "-i", iface, "-m", "conntrack", "--ctstate", "NEW"}, append(comment, "-j", "CONNMARK", "--set-xmark", tunConnectionMark+"/"+tunConnectionMask)...)},
			{Name: "iptables", Args: append([]string{"-t", legacyTUNIPTablesTable, "-A", tunPreroutingChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark", "--nfmask", tunConnectionMask, "--ctmask", tunConnectionMask)...)},
		},
		tunOutputChain: {
			{Name: "iptables", Args: append([]string{"-t", legacyTUNIPTablesTable, "-A", tunOutputChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark", "--nfmask", tunConnectionMask, "--ctmask", tunConnectionMask)...)},
		},
		tunForwardChain: {
			{Name: "iptables", Args: append([]string{"-t", legacyTUNIPTablesTable, "-A", tunForwardChain, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED"}, append(comment, "-j", "CONNMARK", "--restore-mark", "--nfmask", tunConnectionMask, "--ctmask", tunConnectionMask)...)},
		},
	}
}

func ensureLegacyTUNChain(chain string) error {
	if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-S", chain}}); err == nil {
		_, err = runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-F", chain}})
		return err
	} else if !isTUNNotFoundError(err) {
		return fmt.Errorf("检查 legacy iptables 链 %s 失败: %w", chain, err)
	}
	_, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-N", chain}})
	return err
}

func ensureLegacyTUNMainJump(command tunCommand) error {
	deleteCommand := command
	deleteCommand.Args = append([]string{}, command.Args...)
	for i, arg := range deleteCommand.Args {
		if arg == "-A" {
			deleteCommand.Args[i] = "-D"
			break
		}
	}
	for {
		_, err := runTUNCommand(deleteCommand)
		if err == nil {
			continue
		}
		if !isTUNNotFoundError(err) {
			return err
		}
		break
	}
	_, err := runTUNCommand(command)
	return err
}

func setupLegacyTUNIPTables(iface string) error {
	for _, chain := range tunChainNames() {
		if err := ensureLegacyTUNChain(chain); err != nil {
			return err
		}
	}
	for _, command := range tunMainJumpCommands() {
		if err := ensureLegacyTUNMainJump(command); err != nil {
			return err
		}
	}
	for _, commands := range tunChainRuleCommands(iface) {
		for _, command := range commands {
			if _, err := runTUNCommand(command); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanupLegacyTUNIPTables() error {
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
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-S", chain}}); err != nil {
			if !isTUNNotFoundError(err) {
				errs = append(errs, fmt.Errorf("检查 legacy mihomo-tui 链 %s 失败: %w", chain, err))
			}
			continue
		}
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-F", chain}}); err != nil {
			errs = append(errs, err)
			continue
		}
		if _, err := runTUNCommand(tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-X", chain}}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cleanupTUNFirewall(backend string, state tunRoutingState) error {
	switch backend {
	case tunFirewallBackendNFT:
		var errs []error
		if state.isOwnedCurrentNFTTable() {
			if err := cleanupTUNNFTFirewall(state); err != nil {
				errs = append(errs, err)
			}
		} else {
			Debugf("未找到当前 TUN 状态所有权证明；不删除 %s 表", tunNFTTable)
		}
		// 历史专用链具有固定项目名称和跳转引用，可通过 nft JSON 精确迁移。
		if err := cleanupLegacyTUNArtifactsWithNFT(); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	case tunFirewallBackendIPTablesLegacy:
		return cleanupLegacyTUNIPTables()
	default:
		return fmt.Errorf("未知 TUN 防火墙后端: %q", backend)
	}
}

func setupTUNFirewall(backend, iface string, state tunRoutingState) error {
	switch backend {
	case tunFirewallBackendNFT:
		return setupTUNNFTFirewall(iface, state)
	case tunFirewallBackendIPTablesLegacy:
		return setupLegacyTUNIPTables(iface)
	default:
		return fmt.Errorf("未知 TUN 防火墙后端: %q", backend)
	}
}

func cleanupTUNRoutingWithBackend(backend string) error {
	Infof("[cleanupTUNRouting] 正在清理 mihomo-tui TUN 路由规则: firewall=%s", backend)
	var errs []error
	policyClean := true
	state, stateErr := loadTUNRoutingState()
	if stateErr != nil {
		policyClean = false
		errs = append(errs, stateErr)
	} else if commands, err := tunPolicyCleanupCommands(state); err != nil {
		policyClean = false
		errs = append(errs, err)
	} else {
		for _, command := range commands {
			if err := runTUNCommandIgnoringNotFound(command); err != nil {
				policyClean = false
				errs = append(errs, err)
			}
		}
	}

	firewallClean := true
	if err := cleanupTUNFirewall(backend, state); err != nil {
		firewallClean = false
		errs = append(errs, err)
	}
	if stateErr == nil && policyClean && firewallClean {
		if err := os.Remove(tunRoutingStatePath()); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("删除 TUN 路由状态失败: %w", err))
		}
	}
	return errors.Join(errs...)
}

func cleanupTUNRoutingLocked() error {
	state, stateErr := loadTUNRoutingState()
	if stateErr != nil {
		return stateErr
	}
	backend, err := detectTUNFirewallBackend()
	if err != nil {
		return err
	}
	if state.FirewallBackend == tunFirewallBackendNFT && backend != tunFirewallBackendNFT {
		return fmt.Errorf("TUN 状态由 nft 后端创建，但当前未找到 nft 命令，无法安全清理项目防火墙表")
	}
	return cleanupTUNRoutingWithBackend(backend)
}

func cleanupTUNRouting() error {
	return withTUNRoutingLock(cleanupTUNRoutingLocked)
}

func rollbackTUNSetup(setupErr error, backend string) error {
	if cleanupErr := cleanupTUNRoutingWithBackend(backend); cleanupErr != nil {
		return errors.Join(setupErr, fmt.Errorf("自动回滚失败；请执行 mihomo-tui tun_diagnose 检查项目状态: %w", cleanupErr))
	}
	return setupErr
}

func verifyTUNRouting(backend string) error {
	verification := []tunCommand{
		{Name: "ip", Args: []string{"-4", "rule", "show"}},
		{Name: "ip", Args: []string{"-4", "route", "show", "table", tunRoutingTable}},
	}
	if backend == tunFirewallBackendNFT {
		verification = append(verification, tunCommand{Name: "nft", Args: []string{"list", "table", tunNFTFamily, tunNFTTable}})
	} else {
		for _, chain := range tunChainNames() {
			verification = append(verification, tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-S", chain}})
		}
	}
	for _, command := range verification {
		if _, err := runTUNCommand(command); err != nil {
			return fmt.Errorf("验证 TUN 回包规则失败: %w", err)
		}
	}
	return nil
}

func setupTUNRoutingLocked() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("TUN 路由修复需要 root 权限")
	}
	backend, err := detectTUNFirewallBackend()
	if err != nil {
		return fmt.Errorf("选择 TUN 防火墙后端失败: %w", err)
	}

	// 先做只读预检。发现其他 TUN 接管时必须在任何清理/安装动作前退出。
	preflight, err := collectTUNPreflight(backend)
	if err != nil {
		return fmt.Errorf("TUN 路由预检失败: %w", err)
	}
	if err := preflight.validateForApply(); err != nil {
		return err
	}
	// 只清理状态文件能够证明属于本项目的资源；成功后表和优先级必须为空，
	// 才允许继续创建，避免覆盖 VPN、Docker 或另一套策略路由。
	if err := cleanupTUNRoutingWithBackend(backend); err != nil {
		return fmt.Errorf("清理旧 mihomo-tui TUN 规则失败；请执行 mihomo-tui tun_diagnose 检查: %w", err)
	}
	if err := validateTUNRouteSlotsAreFree(); err != nil {
		return err
	}
	if preflight.IPv6Warning {
		Warnf("检测到 IPv6 默认路由；当前 TUN 回包修复仅覆盖 IPv4，IPv6 入站服务不受此规则保护")
	}

	state, err := newTUNRoutingState(preflight.Interface, preflight.Gateway, backend)
	if err != nil {
		return err
	}
	// preparing 状态必须先持久化。系统任一步骤中断时，下一次启动/停止可安全回收。
	if err := saveTUNRoutingState(state); err != nil {
		return fmt.Errorf("保存 mihomo-tui TUN 预备状态失败: %w", err)
	}
	if err := ensureTUNDefaultRoute(preflight.Interface, preflight.Gateway); err != nil {
		return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui 策略路由失败: %w", err), backend)
	}
	for _, command := range tunPolicyRuleAddCommands() {
		if err := runTUNRuleAddIgnoringExists(command); err != nil {
			return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui 策略规则失败: %w", err), backend)
		}
	}
	if err := setupTUNFirewall(backend, preflight.Interface, state); err != nil {
		return rollbackTUNSetup(fmt.Errorf("设置 mihomo-tui %s 防火墙规则失败: %w", backend, err), backend)
	}
	if err := verifyTUNRouting(backend); err != nil {
		return rollbackTUNSetup(err, backend)
	}
	state.Phase = tunRoutingStateActive
	if err := saveTUNRoutingState(state); err != nil {
		return rollbackTUNSetup(fmt.Errorf("保存 mihomo-tui TUN 活动状态失败: %w", err), backend)
	}

	Infof("[SetupTUNRouting] TUN 路由修复已设置: iface=%s gateway=%s firewall=%s table=%s mark=%s/%s", preflight.Interface, preflight.Gateway, backend, tunRoutingTable, tunConnectionMark, tunConnectionMask)
	return nil
}

// SetupTUNRouting installs the IPv4 return-path protection before the mihomo
// core starts. Any preflight or installation failure is fail-closed: the core
// must not enter TUN mode without the matching return-path rules.
func SetupTUNRouting() error {
	return withTUNRoutingLock(setupTUNRoutingLocked)
}

// DebugTUNRouting writes the complete read-only preflight to w. Pass true to
// apply the plan after the preflight; without it no network state is changed.
func DebugTUNRouting(w io.Writer, apply ...bool) error {
	if w == nil {
		return fmt.Errorf("TUN 调试输出流不能为空")
	}
	doApply := len(apply) > 0 && apply[0]
	tunDebugSessionMu.Lock()
	defer tunDebugSessionMu.Unlock()
	restoreWriter := setTUNDebugWriter(w)
	defer restoreWriter()

	writeTUNDebugf("开始 TUN 路由调试，mode=%s uid=%d config_dir=%s", map[bool]string{true: "apply", false: "read-only"}[doApply], os.Geteuid(), GetConfigDir())
	lines, err := DescribeTUNRouting()
	for _, line := range lines {
		writeTUNDebugf("%s", line)
	}
	if err != nil {
		writeTUNDebugf("TUN 路由预检失败: %v", err)
		return err
	}
	if !doApply {
		writeTUNDebugf("只读调试完成；未修改路由或防火墙。使用 tun_debug --apply 才会执行安装")
		return nil
	}
	if err := SetupTUNRouting(); err != nil {
		writeTUNDebugf("TUN 路由修复失败: %v", err)
		return err
	}
	writeTUNDebugf("TUN 路由修复及验证完成；规则保持启用状态")
	return nil
}

// DescribeTUNRouting returns a read-only preflight report and the intended
// commands. The returned lines remain useful even when a safety preflight
// rejects applying the plan.
func DescribeTUNRouting() ([]string, error) {
	backend, err := detectTUNFirewallBackend()
	if err != nil {
		return nil, err
	}
	preflight, err := collectTUNPreflight(backend)
	if err != nil {
		return nil, err
	}
	state, stateErr := loadTUNRoutingState()
	if stateErr != nil {
		return nil, stateErr
	}
	result := []string{
		fmt.Sprintf("防火墙后端: %s", preflight.Backend),
		fmt.Sprintf("默认 IPv4 路由: iface=%s gateway=%s", preflight.Interface, preflight.Gateway),
		"回包 mark: " + tunConnectionMark + "/" + tunConnectionMask + "（只操作项目 bit，保留其他 fwmark）",
	}
	if state.Phase != "" {
		result = append(result, fmt.Sprintf("项目状态: version=%d phase=%s backend=%s created=%s", state.Version, state.Phase, state.FirewallBackend, state.CreatedAt))
	} else {
		result = append(result, "项目状态: 未发现已持久化的 TUN 路由状态")
	}
	if len(preflight.ExternalTUNs) > 0 {
		result = append(result, "冲突: 检测到其他活跃 TUN 默认路由: "+strings.Join(preflight.ExternalTUNs, ", "))
	}
	if preflight.IPv6Warning {
		result = append(result, "警告: 检测到 IPv6 默认路由；本修复只覆盖 IPv4")
	}
	result = append(result, tunCommand{Name: "ip", Args: []string{"route", "add", "default", "via", preflight.Gateway, "dev", preflight.Interface, "table", tunRoutingTable}}.String())
	for _, command := range tunPolicyRuleAddCommands() {
		result = append(result, command.String())
	}
	if backend == tunFirewallBackendNFT {
		script, err := buildTUNNFTScript(preflight.Interface)
		if err != nil {
			return result, err
		}
		result = append(result, "nft -f - <<'EOF'")
		result = append(result, strings.Split(strings.TrimSuffix(script, "\n"), "\n")...)
		result = append(result, "EOF")
	} else {
		for _, chain := range tunChainNames() {
			result = append(result, tunCommand{Name: "iptables", Args: []string{"-t", legacyTUNIPTablesTable, "-N", chain}}.String())
		}
		for _, command := range tunMainJumpCommands() {
			result = append(result, command.String())
		}
		for _, chainCommands := range tunChainRuleCommands(preflight.Interface) {
			for _, command := range chainCommands {
				result = append(result, command.String())
			}
		}
	}
	return result, nil
}

// RestoreTUNRouting cleans only routes and firewall objects proven to be owned
// by mihomo-tui. It is safe to call repeatedly after stop or a failed start.
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
