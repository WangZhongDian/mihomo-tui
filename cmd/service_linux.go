//go:build linux

package cmd

import (
	"fmt"
	"os"
	"os/exec"
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

	// 3. 获取当前可执行文件路径
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 4. 确定目标安装路径
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

	// 5. 以 root 用户运行服务
	runUser := "root"

	// 6. 生成 systemd unit 文件
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

	// 7. daemon-reload
	if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl daemon-reload")

	// 8. enable 服务
	if output, err := exec.Command("systemctl", "enable", "mihomo-tui").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable 失败: %w, 输出: %s", err, string(output))
	}
	mihomotui.Infof("已执行 systemctl enable mihomo-tui")

	// 9. 启动服务
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
