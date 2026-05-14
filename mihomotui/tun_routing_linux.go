//go:build linux

package mihomotui

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// getDefaultGateway 解析系统默认路由，返回物理网卡名和网关 IP
func getDefaultGateway() (iface, gateway string, err error) {
	out, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("获取默认路由失败: %w, output: %s", err, string(out))
	}
	lines := strings.SplitSeq(string(out), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		for i := 0; i < len(parts)-1; i++ {
			if parts[i] == "dev" {
				iface = parts[i+1]
			}
			if parts[i] == "via" {
				gateway = parts[i+1]
			}
		}
		if iface != "" && gateway != "" {
			return iface, gateway, nil
		}
	}
	return "", "", fmt.Errorf("无法从路由表解析默认网关: %s", string(out))
}

// deleteConnmarkRulesByLine 按行号删除 iptables mangle 链中所有 CONNMARK 规则
func deleteConnmarkRulesByLine(chain string) {
	out, _ := exec.Command("iptables", "-t", "mangle", "-L", chain, "-n", "--line-numbers").CombinedOutput()
	lines := strings.Split(string(out), "\n")
	var nums []int
	for _, line := range lines {
		if !strings.Contains(line, "CONNMARK") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			nums = append(nums, n)
		}
	}
	// 从后往前删，避免行号变化
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))
	for _, n := range nums {
		_ = exec.Command("iptables", "-t", "mangle", "-D", chain, strconv.Itoa(n)).Run()
	}
}

// cleanupTUNRouting 清理所有由本工具设置的 ip rule / ip route / iptables 规则
func cleanupTUNRouting() error {
	// 1. 清理 ip rule（忽略不存在时的错误）
	Infof("[cleanupTUNRouting] 正在清理旧 TUN 路由规则...")
	_ = exec.Command("ip", "rule", "del", "to", "10.0.0.0/8", "table", "main", "pref", "100").Run()
	_ = exec.Command("ip", "rule", "del", "to", "172.16.0.0/12", "table", "main", "pref", "100").Run()
	_ = exec.Command("ip", "rule", "del", "to", "192.168.0.0/16", "table", "main", "pref", "100").Run()
	_ = exec.Command("ip", "rule", "del", "fwmark", "0x100", "table", "100", "pref", "200").Run()
	_ = exec.Command("ip", "rule", "del", "fwmark", "0x100", "table", "100").Run() // 兜底删除（不带 pref）

	// 2. 清理 ip route table 100
	_ = exec.Command("ip", "route", "del", "default", "table", "100").Run()

	// 3. 清理 iptables mangle 中所有 CONNMARK 规则（通过列出规则并按行号删除，不依赖网卡名）
	deleteConnmarkRulesByLine("PREROUTING")
	deleteConnmarkRulesByLine("OUTPUT")
	deleteConnmarkRulesByLine("FORWARD")

	return nil
}

// SetupTUNRouting 设置 TUN 模式下的路由修复规则，解决外部无法访问服务器开放端口的问题。
// 调用前会自动清理旧规则，保证幂等性。
func SetupTUNRouting() error {
	iface, gateway, err := getDefaultGateway()
	if err != nil {
		return fmt.Errorf("获取默认网关失败: %w", err)
	}

	Infof("[SetupTUNRouting] 默认网关: iface=%s gateway=%s", iface, gateway)
	Infof("[SetupTUNRouting] 正在设置 TUN 路由修复规则...")

	// 先清理旧规则（幂等：即使之前 crash 残留了规则也能重置）
	if err := cleanupTUNRouting(); err != nil {
		Warnf("[cleanupTUNRouting] 清理旧 TUN 路由规则时出现问题: %v", err)
	}

	// 1. 创建 100 号路由表，指定从物理网卡原路返回
	if out, err := exec.Command("ip", "route", "add", "default", "via", gateway, "dev", iface, "table", "100").CombinedOutput(); err != nil {
		return fmt.Errorf("设置 ip route table 100 失败: %w, output: %s", err, string(out))
	}

	// 2. 设置策略路由
	rules := [][]string{
		{"ip", "rule", "add", "to", "10.0.0.0/8", "table", "main", "pref", "100"},
		{"ip", "rule", "add", "to", "172.16.0.0/12", "table", "main", "pref", "100"},
		{"ip", "rule", "add", "to", "192.168.0.0/16", "table", "main", "pref", "100"},
		{"ip", "rule", "add", "fwmark", "0x100", "table", "100", "pref", "200"},
	}
	for _, args := range rules {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			Warnf("[SetupTUNRouting] 设置 ip rule 失败: %v, output: %s", err, string(out))
		}
	}

	// 3. 设置 iptables CONNMARK（PREROUTING 打标记 + OUTPUT/FORWARD 恢复标记）
	// Docker 容器回包走 FORWARD 链不走 OUTPUT，所以 FORWARD 链也必须恢复标记
	iptRules := [][]string{
		{"iptables", "-t", "mangle", "-A", "PREROUTING", "-i", iface, "-m", "conntrack", "--ctstate", "NEW", "-j", "CONNMARK", "--set-mark", "0x100"},
		{"iptables", "-t", "mangle", "-A", "PREROUTING", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "CONNMARK", "--restore-mark"},
		{"iptables", "-t", "mangle", "-A", "OUTPUT", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "CONNMARK", "--restore-mark"},
		{"iptables", "-t", "mangle", "-A", "FORWARD", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "CONNMARK", "--restore-mark"},
	}
	for _, args := range iptRules {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			Warnf("[SetupTUNRouting] 设置 iptables 失败: %v, output: %s", err, string(out))
		}
	}

	Infof("[SetupTUNRouting] TUN 路由修复已设置: iface=%s gateway=%s", iface, gateway)
	return nil
}

// RestoreTUNRouting 清理所有 TUN 路由修复规则，将系统网络恢复到 TUN 开启前的状态。
func RestoreTUNRouting() error {
	if err := cleanupTUNRouting(); err != nil {
		Warnf("清理 TUN 路由规则时出现问题: %v", err)
	}
	Infof("[RestoreTUNRouting] TUN 路由规则已清理")
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
