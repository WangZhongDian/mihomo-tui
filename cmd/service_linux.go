//go:build linux

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"text/template"

	"mihomotui/mihomotui"
)

// InstallService 将当前程序安装为 systemd 系统服务
// 需要 root 权限执行
func InstallService() error {
	// 1. 检查 root 权限
	if os.Geteuid() != 0 {
		return fmt.Errorf("安装系统服务需要 root 权限，请使用 sudo 运行")
	}

	// 2. 若服务正在运行，先停止并清理环境
	if output, _ := exec.Command("systemctl", "is-active", "mihomo-tui").CombinedOutput(); strings.TrimSpace(string(output)) == "active" {
		mihomotui.Infof("检测到服务正在运行，先停止...")
		if output, err := exec.Command("systemctl", "stop", "mihomo-tui").CombinedOutput(); err != nil {
			return fmt.Errorf("停止现有服务失败: %w, 输出: %s", err, string(output))
		}
		mihomotui.Infof("已停止现有服务")
		// 停止后手动清理环境（ExecStop 可能未触发或已执行）
		mihomotui.CleanupEnvironment()
	}

	// 3. 确保 root daemon 的 IPC 授权组存在：mihomo-tui 仅允许读取状态，
	// mihomo-tui-operator 可以管理订阅；系统级操作仍由 daemon 的 peer credential 限制。
	if err := ensureIPCAuthorizationGroups(); err != nil {
		return err
	}
	if userName := os.Getenv("SUDO_USER"); userName != "" && userName != "root" {
		if err := AddIPCOperator(userName); err != nil {
			return err
		}
		fmt.Printf("   已验证用户 %s 已加入 IPC 授权组。\n", userName)
		fmt.Println("   组权限只会在新的登录会话中生效；请注销桌面会话并重新登录。")
		fmt.Println("   临时生效可执行: newgrp mihomo-tui")
	}

	// 4. 获取当前可执行文件路径
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 5. 确定目标安装路径
	targetPath := "/usr/local/bin/mihomo-tui"

	// 如果当前路径不是目标路径，则复制
	if execPath != targetPath {
		data, err := os.ReadFile(execPath)
		if err != nil {
			return fmt.Errorf("读取可执行文件失败: %w", err)
		}
		if err := os.WriteFile(targetPath, data, 0755); err != nil {
			return fmt.Errorf("复制可执行文件到 %s 失败: %w", targetPath, err)
		}
		mihomotui.Infof("已复制可执行文件到 %s", targetPath)
	}

	// 6. 以 root 用户运行服务
	runUser := "root"

	// 7. 生成 systemd unit 文件
	unitPath := "/etc/systemd/system/mihomo-tui.service"
	tmpl, err := template.New("unit").Parse(ServiceUnitTemplate)
	if err != nil {
		return fmt.Errorf("解析服务模板失败: %w", err)
	}

	f, err := os.Create(unitPath)
	if err != nil {
		return fmt.Errorf("创建服务文件 %s 失败: %w", unitPath, err)
	}
	defer f.Close()

	data := struct {
		ExecPath string
		User     string
	}{
		ExecPath: targetPath,
		User:     runUser,
	}
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("渲染服务模板失败: %w", err)
	}
	mihomotui.Infof("已写入服务文件: %s", unitPath)

	// 8. daemon-reload
	if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl daemon-reload")

	// 9. enable 服务
	if output, err := exec.Command("systemctl", "enable", "mihomo-tui").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable 失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl enable mihomo-tui")

	// 10. 启动服务
	if output, err := exec.Command("systemctl", "start", "mihomo-tui").CombinedOutput(); err != nil {
		return fmt.Errorf("启动服务失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl start mihomo-tui")

	fmt.Println("✅ mihomo-tui 系统服务安装成功")
	fmt.Printf("   服务文件: %s\n", unitPath)
	fmt.Printf("   运行用户: %s\n", runUser)
	fmt.Printf("   可执行文件: %s\n", targetPath)
	fmt.Println("")
	fmt.Println("使用以下命令管理服务:")
	fmt.Println("   sudo systemctl start   mihomo-tui")
	fmt.Println("   sudo systemctl stop    mihomo-tui")
	fmt.Println("   sudo systemctl restart mihomo-tui")
	fmt.Println("   sudo systemctl status  mihomo-tui")
	return nil
}

// ensureIPCAuthorizationGroups 创建 root daemon 使用的最小权限 IPC 授权组。
// mihomo-tui 组成员可读取不含敏感信息的状态；mihomo-tui-operator 组成员可管理订阅。
func ensureIPCAuthorizationGroups() error {
	for _, groupName := range []string{"mihomo-tui", "mihomo-tui-operator"} {
		if _, err := user.LookupGroup(groupName); err == nil {
			continue
		}
		if output, err := exec.Command("groupadd", "--system", groupName).CombinedOutput(); err != nil {
			// groupadd 可能与其他安装过程并发；再次查询确认即可。
			if _, lookupErr := user.LookupGroup(groupName); lookupErr == nil {
				continue
			}
			return fmt.Errorf("创建 IPC 授权组 %s 失败: %w, 输出: %s", groupName, err, string(output))
		}
		mihomotui.Infof("已创建 IPC 授权组: %s", groupName)
	}
	return nil
}

// AddIPCReader 将指定用户加入只读 IPC 授权组。
func AddIPCReader(userName string) error {
	return addIPCUserToGroups(userName, "mihomo-tui")
}

// AddIPCOperator 将指定用户加入可管理订阅的 IPC 授权组。
func AddIPCOperator(userName string) error {
	return addIPCUserToGroups(userName, "mihomo-tui", "mihomo-tui-operator")
}

func addIPCUserToGroups(userName string, groups ...string) error {
	if userName == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("管理 IPC 授权组需要 root 权限")
	}
	if err := ensureIPCAuthorizationGroups(); err != nil {
		return err
	}
	if _, err := user.Lookup(userName); err != nil {
		return fmt.Errorf("用户 %s 不存在: %w", userName, err)
	}
	if output, err := exec.Command("usermod", "-aG", strings.Join(groups, ","), userName).CombinedOutput(); err != nil {
		return fmt.Errorf("将用户 %s 加入 IPC 授权组失败: %w, 输出: %s", userName, err, string(output))
	}
	if err := verifyIPCGroupMembership(userName, groups...); err != nil {
		return err
	}
	mihomotui.Infof("已授予 IPC 权限: user=%s groups=%s", userName, strings.Join(groups, ","))
	return nil
}

// verifyIPCGroupMembership 确认组数据库已经写入。usermod 返回 0 但 NSS/组数据库未更新时，
// 不能继续向用户报告“授权成功”，否则后续排查会非常困难。
func verifyIPCGroupMembership(userName string, groups ...string) error {
	output, err := exec.Command("id", "-nG", userName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("验证用户 %s 的 IPC 授权组失败: %w, 输出: %s", userName, err, string(output))
	}
	memberships := make(map[string]bool)
	for _, name := range strings.Fields(string(output)) {
		memberships[name] = true
	}
	for _, groupName := range groups {
		if !memberships[groupName] {
			return fmt.Errorf("usermod 已执行，但系统组数据库中用户 %s 仍未加入 %s；请检查 /etc/group、NSS 配置或 usermod 输出", userName, groupName)
		}
	}
	return nil
}

// IPCOperatorGroupGID 返回操作组 GID，用于诊断和测试。
func IPCOperatorGroupGID() (int, error) {
	group, err := user.LookupGroup("mihomo-tui-operator")
	if err != nil {
		return 0, err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, err
	}
	return gid, nil
}

// UninstallService 卸载 systemd 系统服务
// 需要 root 权限执行
func UninstallService() error {
	// 1. 检查 root 权限
	if os.Geteuid() != 0 {
		return fmt.Errorf("卸载系统服务需要 root 权限，请使用 sudo 运行")
	}

	unitPath := "/etc/systemd/system/mihomo-tui.service"
	binPath := "/usr/local/bin/mihomo-tui"

	// 2. 若服务正在运行，先停止并清理环境
	if output, _ := exec.Command("systemctl", "is-active", "mihomo-tui").CombinedOutput(); strings.TrimSpace(string(output)) == "active" {
		mihomotui.Infof("检测到服务正在运行，先停止...")
		if output, err := exec.Command("systemctl", "stop", "mihomo-tui").CombinedOutput(); err != nil {
			return fmt.Errorf("停止服务失败: %w, 输出: %s", err, string(output))
		}
		mihomotui.Infof("已停止服务")
	}
	// 手动清理环境（systemd stop 已触发 ExecStop，再做一次兜底）
	mihomotui.CleanupEnvironment()

	// 3. disable 服务
	if _, err := os.Stat(unitPath); err == nil {
		if output, err := exec.Command("systemctl", "disable", "mihomo-tui").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl disable 失败: %w, 输出: %s", err, string(output))
		}
		mihomotui.Infof("已执行 systemctl disable mihomo-tui")
	}

	// 4. 删除服务文件
	if _, err := os.Stat(unitPath); err == nil {
		if err := os.Remove(unitPath); err != nil {
			return fmt.Errorf("删除服务文件 %s 失败: %w", unitPath, err)
		}
		mihomotui.Infof("已删除服务文件: %s", unitPath)
	}

	// 5. 删除可执行文件
	if _, err := os.Stat(binPath); err == nil {
		if err := os.Remove(binPath); err != nil {
			return fmt.Errorf("删除可执行文件 %s 失败: %w", binPath, err)
		}
		mihomotui.Infof("已删除可执行文件: %s", binPath)
	}

	// 6. daemon-reload
	if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl daemon-reload")

	fmt.Println("✅ mihomo-tui 系统服务已卸载")
	return nil
}
