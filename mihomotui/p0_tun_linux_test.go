//go:build linux

package mihomotui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeTUNCommandExecutor struct {
	output string
	err    error
	seen   tunCommand
	input  string
}

func (f *fakeTUNCommandExecutor) Run(command tunCommand, input string) (string, error) {
	f.seen = command
	f.input = input
	return f.output, f.err
}

func TestTUNCommandExecutorCanBeInjected(t *testing.T) {
	fake := &fakeTUNCommandExecutor{output: "fake-output"}
	restore := setTUNCommandExecutorForTest(fake)
	defer restore()
	out, err := runTUNCommandInput(tunCommand{Name: "nft", Args: []string{"-f", "-"}}, "script")
	if err != nil || out != "fake-output" {
		t.Fatalf("runTUNCommandInput() = (%q, %v)", out, err)
	}
	if fake.seen.Name != "nft" || fake.input != "script" {
		t.Fatalf("fake executor did not receive command/input: %+v %q", fake.seen, fake.input)
	}
}

func TestTUNCommandDebugTraceIncludesCommandAndOutput(t *testing.T) {
	var output bytes.Buffer
	restore := setTUNDebugWriter(&output)
	defer restore()
	if _, err := runTUNCommand(tunCommand{Name: "printf", Args: []string{"trace-ok"}}); err != nil {
		t.Fatalf("runTUNCommand() error = %v", err)
	}
	trace := output.String()
	for _, expected := range []string{"[tun-debug] $ printf trace-ok", "[tun-debug] output: trace-ok", "[tun-debug] command succeeded"} {
		if !strings.Contains(trace, expected) {
			t.Fatalf("debug trace missing %q:\n%s", expected, trace)
		}
	}
}

func TestTUNDebugSuppressesFullNFTJSONRuleset(t *testing.T) {
	command := tunCommand{Name: "nft", Args: []string{"-j", "-a", "list", "ruleset"}}
	if !suppressTUNDebugOutput(command) {
		t.Fatal("full nft JSON ruleset must be omitted from stdout debug logs")
	}
	if suppressTUNDebugOutput(tunCommand{Name: "nft", Args: []string{"list", "table", "ip", "mihomo_tui"}}) {
		t.Fatal("project table verification output should remain visible")
	}
	if !suppressTUNDebugOutput(tunCommand{Name: "ip", Args: []string{"-j", "-4", "route", "show", "table", "all"}}) {
		t.Fatal("large ip JSON diagnostics must be summarized")
	}
}

func TestTUNNativeNFTScriptUsesDedicatedTableAndMarks(t *testing.T) {
	script, err := buildTUNNFTScript("eth0")
	if err != nil {
		t.Fatalf("buildTUNNFTScript() error = %v", err)
	}
	for _, expected := range []string{
		"add table ip mihomo_tui",
		"type filter hook prerouting priority filter",
		"type route hook output priority filter",
		"type filter hook forward priority filter",
		`iifname "eth0" ct state new ct mark set ct mark | 0x100`,
		"ct state established,related ct mark & 0x100 != 0 meta mark set meta mark | 0x100",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("native nft script missing %q:\n%s", expected, script)
		}
	}
	if strings.Contains(script, "MIHOMO_TUI_") || strings.Contains(script, "table ip mangle") {
		t.Fatalf("native nft script still depends on legacy shared chains:\n%s", script)
	}
	if strings.Contains(script, "| (ct mark") {
		t.Fatalf("native nft script uses an Ubuntu-incompatible dynamic right operand:\n%s", script)
	}
}

func TestTUNNativeNFTScriptRejectsUnsafeInterface(t *testing.T) {
	for _, iface := range []string{"", "eth0;delete table ip filter", "interface-name-is-too-long"} {
		if _, err := buildTUNNFTScript(iface); err == nil {
			t.Fatalf("buildTUNNFTScript(%q) unexpectedly succeeded", iface)
		}
	}
	for _, iface := range []string{"eth0", "enp2s0.10", "veth-ab_1", "eth0:1", "veth0@if2"} {
		if _, err := buildTUNNFTScript(iface); err != nil {
			t.Fatalf("buildTUNNFTScript(%q) error = %v", iface, err)
		}
	}
}

func TestSelectTUNFirewallBackend(t *testing.T) {
	tests := []struct {
		name              string
		nftAvailable      bool
		iptablesAvailable bool
		version           string
		want              string
		wantErr           bool
	}{
		{name: "nft preferred", nftAvailable: true, iptablesAvailable: true, version: "iptables v1.8.7 (nf_tables)", want: tunFirewallBackendNFT},
		{name: "explicit legacy", iptablesAvailable: true, version: "iptables v1.8.7 (legacy)", want: tunFirewallBackendIPTablesLegacy},
		{name: "old iptables", iptablesAvailable: true, version: "iptables v1.6.1", want: tunFirewallBackendIPTablesLegacy},
		{name: "nft frontend without nft command", iptablesAvailable: true, version: "iptables v1.8.7 (nf_tables)", wantErr: true},
		{name: "no backend", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectTUNFirewallBackend(tt.nftAvailable, tt.iptablesAvailable, tt.version)
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectTUNFirewallBackend() error = %v, wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("selectTUNFirewallBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTUNNFTablesJSONUsesActualFamilyAndTable(t *testing.T) {
	data := []byte(`{
		"nftables": [
			{"metainfo": {"json_schema_version": 1}},
			{"chain": {"family": "ip", "table": "mangle", "name": "MIHOMO_TUI_PREROUTING", "handle": 50}},
			{"rule": {"family": "ip", "table": "mangle", "chain": "PREROUTING", "handle": 41, "expr": [
				{"counter": {"packets": 12, "bytes": 900}},
				{"jump": {"target": "MIHOMO_TUI_PREROUTING"}}
			]}},
			{"chain": {"family": "inet", "table": "legacy_mangle", "name": "MIHOMO_TUI_OUTPUT", "handle": 51}},
			{"chain": {"family": "inet", "table": "legacy_mangle", "name": "MIHOMO_TUI_FORWARD", "handle": 52}},
			{"rule": {"family": "inet", "table": "legacy_mangle", "chain": "custom_output", "handle": 43, "expr": [
				{"goto": {"target": "MIHOMO_TUI_OUTPUT"}}
			]}},
			{"chain": {"family": "ip", "table": "mangle", "name": "OTHER_CHAIN", "handle": 53}}
		]
	}`)
	chains, rules, err := parseTUNNFTablesJSON(data)
	if err != nil {
		t.Fatalf("parseTUNNFTablesJSON() error = %v", err)
	}
	if len(chains) != 3 {
		t.Fatalf("parseTUNNFTablesJSON() chains=%v, want 3", chains)
	}
	if chains[0] != (nftChainLocation{Family: "ip", Table: "mangle", Chain: tunPreroutingChain}) {
		t.Fatalf("unexpected first chain location: %+v", chains[0])
	}
	if chains[1] != (nftChainLocation{Family: "inet", Table: "legacy_mangle", Chain: tunOutputChain}) {
		t.Fatalf("unexpected second chain location: %+v", chains[1])
	}
	if len(rules) != 2 {
		t.Fatalf("parseTUNNFTablesJSON() rules=%v, want 2", rules)
	}
	if rules[0].Handle != 41 || rules[0].Target != tunPreroutingChain || rules[0].Chain != "PREROUTING" {
		t.Fatalf("unexpected first rule reference: %+v", rules[0])
	}
	if rules[1].Family != "inet" || rules[1].Table != "legacy_mangle" || rules[1].Handle != 43 || rules[1].Target != tunOutputChain {
		t.Fatalf("unexpected second rule reference: %+v", rules[1])
	}
}

func TestParseTUNNFTablesJSONTreatsGhostChainsAsAbsent(t *testing.T) {
	data := []byte(`{"nftables":[
		{"chain":{"family":"ip","table":"mangle","name":"OTHER_CHAIN","handle":1}},
		{"rule":{"family":"ip","table":"mangle","chain":"PREROUTING","handle":2,"expr":[{"jump":{"target":"OTHER_CHAIN"}}]}}
	]}`)
	chains, rules, err := parseTUNNFTablesJSON(data)
	if err != nil {
		t.Fatalf("parseTUNNFTablesJSON() error = %v", err)
	}
	if len(chains) != 0 || len(rules) != 0 {
		t.Fatalf("unrelated/ghost artifacts were selected: chains=%v rules=%v", chains, rules)
	}
}

func TestTUNLegacyRulesUseOnlyDedicatedChainsAndComments(t *testing.T) {
	for _, command := range tunMainJumpCommands() {
		joined := strings.Join(command.Args, " ")
		if !strings.Contains(joined, tunRuleComment) || !strings.Contains(joined, "MIHOMO_TUI_") {
			t.Fatalf("legacy main jump is not project-scoped: %s", command.String())
		}
		if strings.Contains(joined, "-I ") {
			t.Fatalf("legacy main jump must preserve tail ordering: %s", command.String())
		}
	}
	for chain, commands := range tunChainRuleCommands("eth0") {
		if !strings.HasPrefix(chain, "MIHOMO_TUI_") {
			t.Fatalf("unexpected legacy chain %q", chain)
		}
		for _, command := range commands {
			if !strings.Contains(strings.Join(command.Args, " "), tunRuleComment) {
				t.Fatalf("legacy chain rule lacks comment: %s", command.String())
			}
		}
	}
}

func TestTUNPolicyCleanupRequiresPersistedOwnershipState(t *testing.T) {
	commands, err := tunPolicyCleanupCommands(tunRoutingState{})
	if err != nil {
		t.Fatalf("empty state returned error: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("cleanup without state generated %d commands, want 0", len(commands))
	}

	commands, err = tunPolicyCleanupCommands(tunRoutingState{Interface: "eth0", Gateway: "192.0.2.1"})
	if err != nil {
		t.Fatalf("legacy state returned error: %v", err)
	}
	if len(commands) != 5 {
		t.Fatalf("cleanup with legacy state generated %d commands, want 5", len(commands))
	}
	if !strings.Contains(strings.Join(commands[3].Args, " "), "table "+legacyTUNRoutingTable) {
		t.Fatalf("legacy cleanup did not target table %s: %s", legacyTUNRoutingTable, commands[3].String())
	}

	current := tunRoutingState{Interface: "eth0", Gateway: "192.0.2.1", RoutingTable: tunRoutingTable, PrivateRulePref: tunPrivateRulePref, MarkRulePref: tunMarkRulePref, FirewallBackend: tunFirewallBackendNFT}
	commands, err = tunPolicyCleanupCommands(current)
	if err != nil {
		t.Fatalf("current state returned error: %v", err)
	}
	for _, command := range commands {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "CONNMARK") || !strings.Contains(joined, tunRoutingTable) && !strings.Contains(joined, "table main") {
			t.Fatalf("unsafe or unscoped policy cleanup command: %s", command.String())
		}
	}
	if _, err := tunPolicyCleanupCommands(tunRoutingState{Interface: "eth0"}); err == nil {
		t.Fatal("incomplete ownership state unexpectedly permitted policy cleanup")
	}
}

type tunRouteSlotsExecutor struct{}

func (tunRouteSlotsExecutor) Run(command tunCommand, input string) (string, error) {
	if command.Name != "ip" {
		return "", fmt.Errorf("unexpected command: %s", command.String())
	}
	joined := strings.Join(command.Args, " ")
	switch joined {
	case "-4 route show table " + tunRoutingTable:
		return "Error: ipv4: FIB table does not exist.\nDump terminated", errors.New("exit status 2")
	case "-4 rule show":
		return "0: from all lookup local\n32766: from all lookup main\n", nil
	default:
		return "", fmt.Errorf("unexpected ip command: %s", command.String())
	}
}

func TestTUNRouteSlotsTreatNeverCreatedTableAsEmpty(t *testing.T) {
	restore := setTUNCommandExecutorForTest(tunRouteSlotsExecutor{})
	defer restore()
	if err := validateTUNRouteSlotsAreFree(); err != nil {
		t.Fatalf("validateTUNRouteSlotsAreFree() error = %v, want empty route table accepted", err)
	}
}

func TestTUNNotFoundErrorAndRouteTableClassification(t *testing.T) {
	missingChain := errors.New("iptables v1.8.10 (nf_tables): Chain 'MIHOMO_TUI_PREROUTING' does not exist")
	if !isTUNNotFoundError(missingChain) {
		t.Fatal("missing chain must be treated as idempotent cleanup")
	}
	permission := errors.New("iptables: Permission denied (you must be root)")
	if isTUNNotFoundError(permission) {
		t.Fatal("permission error must remain visible")
	}
	missingTable := errors.New("Error: ipv4: FIB table does not exist. Dump terminated")
	if !isTUNRouteTableMissingError(missingTable) {
		t.Fatal("a never-created route table must be treated as empty")
	}
	if isTUNRouteTableMissingError(errors.New("operation not permitted")) {
		t.Fatal("route permission errors must remain visible")
	}
}

func TestTUNRoutingUsesDedicatedPreMihomoPolicyTable(t *testing.T) {
	if tunRoutingTable == legacyTUNRoutingTable {
		t.Fatalf("mihomo-tui table %s collides with mihomo auto-route table", tunRoutingTable)
	}
	if tunPrivateRulePref != "100" || tunMarkRulePref != "200" {
		t.Fatalf("Docker return policy priorities changed: private=%s mark=%s", tunPrivateRulePref, tunMarkRulePref)
	}
}

func TestTUNPolicyRulesPreserveOtherFirewallMarksAndCGNAT(t *testing.T) {
	commands := tunPolicyRuleAddCommands()
	joined := make([]string, 0, len(commands))
	for _, command := range commands {
		joined = append(joined, command.String())
	}
	text := strings.Join(joined, "\n")
	for _, expected := range []string{
		"to 100.64.0.0/10 table main pref " + tunPrivateRulePref,
		"fwmark " + tunConnectionMark + "/" + tunConnectionMask + " table " + tunRoutingTable + " pref " + tunMarkRulePref,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("policy rule plan missing %q:\n%s", expected, text)
		}
	}
}

func TestParseActiveTUNInterfacesDetectsOnlyDefaultRouteTUN(t *testing.T) {
	links := []byte(`[
		{"ifname":"eno1","linkinfo":{"info_kind":"ether"}},
		{"ifname":"Meta","linkinfo":{"info_kind":"tun"}},
		{"ifname":"idle-tun","linkinfo":{"info_kind":"tun"}}
	]`)
	routes := []byte(`[
		{"dst":"default","dev":"eno1"},
		{"dst":"default","dev":"Meta"},
		{"dst":"192.0.2.0/24","dev":"idle-tun"}
	]`)
	got, err := parseActiveTUNInterfaces(links, routes)
	if err != nil {
		t.Fatalf("parseActiveTUNInterfaces() error = %v", err)
	}
	if len(got) != 1 || got[0] != "Meta" {
		t.Fatalf("parseActiveTUNInterfaces() = %v, want [Meta]", got)
	}
}

func TestTUNPreflightRejectsExternalActiveTUN(t *testing.T) {
	err := (tunPreflight{ExternalTUNs: []string{"Meta"}}).validateForApply()
	if err == nil || !strings.Contains(err.Error(), "其他活跃 TUN") {
		t.Fatalf("validateForApply() error = %v, want external TUN rejection", err)
	}
}

func TestFindTUNRulePriorityCollisions(t *testing.T) {
	rules := "0: from all lookup local\n100: from all lookup main\n200: from all fwmark 0x9 lookup 99\n32766: from all lookup main\n"
	got := findTUNRulePriorityCollisions(rules)
	if len(got) != 2 || !strings.Contains(got[0], "100:") || !strings.Contains(got[1], "200:") {
		t.Fatalf("findTUNRulePriorityCollisions() = %v", got)
	}
}

func TestNewTUNRoutingStateIsPreparingAndScoped(t *testing.T) {
	state, err := newTUNRoutingState("eth0", "192.0.2.1", tunFirewallBackendNFT)
	if err != nil {
		t.Fatalf("newTUNRoutingState() error = %v", err)
	}
	if state.Version != tunRoutingStateVersion || state.Phase != tunRoutingStatePreparing || state.InstanceID == "" {
		t.Fatalf("unexpected new state: %+v", state)
	}
	if state.ConnectionMark != tunConnectionMark || state.ConnectionMask != tunConnectionMask || !state.isOwnedCurrentNFTTable() {
		t.Fatalf("new state does not identify the project-owned nft table: %+v", state)
	}
}
