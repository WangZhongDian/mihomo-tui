//go:build linux

package mihomotui

import (
	"errors"
	"strings"
	"testing"
)

func TestTUNRulesUseOnlyDedicatedChainsAndComments(t *testing.T) {
	for _, command := range tunMainJumpCommands() {
		joined := strings.Join(command.Args, " ")
		if !strings.Contains(joined, tunRuleComment) || !strings.Contains(joined, "MIHOMO_TUI_") {
			t.Fatalf("main jump is not project-scoped: %s", command.String())
		}
	}
	for chain, commands := range tunChainRuleCommands("eth0") {
		if !strings.HasPrefix(chain, "MIHOMO_TUI_") {
			t.Fatalf("unexpected chain %q", chain)
		}
		for _, command := range commands {
			joined := strings.Join(command.Args, " ")
			if !strings.Contains(joined, tunRuleComment) {
				t.Fatalf("chain rule lacks comment: %s", command.String())
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
		t.Fatalf("cleanup without state generated %d policy commands, want 0", len(commands))
	}

	commands, err = tunPolicyCleanupCommands(tunRoutingState{Interface: "eth0", Gateway: "192.0.2.1"})
	if err != nil {
		t.Fatalf("complete state returned error: %v", err)
	}
	if len(commands) != 5 {
		t.Fatalf("cleanup with state generated %d commands, want 5", len(commands))
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

func TestTUNNotFoundErrorRecognizesIPTablesNFTMissingChain(t *testing.T) {
	err := errors.New("执行失败: iptables -t mangle -D PREROUTING -j MIHOMO_TUI_PREROUTING: exit status 2; output: iptables v1.8.10 (nf_tables): Chain 'MIHOMO_TUI_PREROUTING' does not exist")
	if !isTUNNotFoundError(err) {
		t.Fatal("iptables-nft missing-chain error must be treated as an idempotent cleanup result")
	}
}

func TestTUNNotFoundErrorDoesNotHidePermissionError(t *testing.T) {
	err := errors.New("执行失败: iptables -t mangle -D PREROUTING: exit status 4; output: Permission denied (you must be root)")
	if isTUNNotFoundError(err) {
		t.Fatal("permission error must remain visible")
	}
}
