# mihomo-tui

mihomo-tui 是 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）的终端 UI 配置工具。基于 [rivo/tview](https://github.com/rivo/tview) 开发，专为 Linux 无桌面服务器环境设计，提供直观的键盘驱动界面来管理代理配置。

[English README](README.md)

## 功能特性

- **代理管理** — 可视化节点列表，支持按延迟排序、多选测速、自动选择最优节点
- **订阅管理** — 支持添加、删除、更新订阅，自动下载并合并配置
- **规则管理** — 查看当前生效规则，支持自定义规则配置
- **连接监控** — 实时查看活跃连接、流量统计与连接详情
- **延迟测试** — 批量节点延迟测试，可视化结果排序
- **系统代理** — 一键开启/关闭系统代理（HTTP/SOCKS5）
- **TUN 模式** — 支持 TUN 模式与路由配置
- **日志查看** — 实时滚动日志，支持过滤与暂停
- **systemd 服务** — 内置服务安装/卸载，开机自启支持
- **动态分页** — 终端尺寸变化时自动适配表单和代理列表分页

## 系统要求

- **操作系统**: Linux（amd64 / arm64 / armv7 / 386）
- **运行模式**: 需 root 权限安装服务；TUI 客户端普通用户可运行

## 快速安装

使用官方一键安装脚本（推荐）：

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/WangZhongDian/mihomo-tui/main/scripts/install.sh)"
```

指定版本安装：

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/WangZhongDian/mihomo-tui/main/scripts/install.sh)" -s -- -v v0.1.0
```

安装完成后会自动注册 systemd 服务并启动。

## 手动安装

1. 前往 [Releases](https://github.com/WangZhongDian/mihomo-tui/releases) 页面下载对应架构的二进制文件
2. 将可执行文件放入系统 PATH（如 `/usr/local/bin`）
3. 安装 systemd 服务：

```bash
sudo mihomo-tui install_service
```

## 使用方法

### 启动 TUI 客户端

```bash
mihomo-tui
```

### 命令行选项

```
mihomo-tui — mihomo 终端 UI 配置工具

用法:
  mihomo-tui [选项]              启动 TUI 客户端
  mihomo-tui server [选项]       启动后台 IPC 服务
  mihomo-tui install_service     安装为 systemd 服务（需 root）
  mihomo-tui uninstall           卸载 systemd 服务（需 root）
  mihomo-tui version             显示版本信息

选项:
  -d string    指定配置目录
  -standalone  启动嵌入式服务端（一体模式）
```

### 常用命令

```bash
# 查看服务状态
sudo systemctl status mihomo-tui

# 停止/重启服务
sudo systemctl stop mihomo-tui
sudo systemctl restart mihomo-tui

# 卸载服务
sudo mihomo-tui uninstall
```

## 界面导航

启动 TUI 后，使用以下快捷键操作：

| 快捷键 | 说明 |
|--------|------|
| `Tab` | 切换焦点 |
| `↑/↓` | 上下移动 |
| `Enter` | 确认选择 |
| `Esc` / `q` | 返回/退出 |
| `PgUp` / `PgDn` | 表单/列表翻页 |
| `Space` | 复选框选中/取消 |

## 项目结构

```
mihomo-tui/
├── cmd/              # 命令行入口
├── mihomotui/        # 核心逻辑
│   ├── ui/           # TUI 界面（tview）
│   ├── config.go     # 配置管理
│   ├── daemon*.go    # IPC 服务与处理器
│   └── ...
├── scripts/          # 安装脚本
├── .github/workflows/# CI/CD
├── main.go           # 主入口
└── go.mod
```

## 构建

需要 Go 1.26+：

```bash
go build -ldflags="-s -w" -o mihomo-tui .
```

交叉编译示例（ARM64）：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o mihomo-tui-linux-arm64 .
```

## 发布流程

本项目使用 GitHub Actions 自动构建和发布：

1. 本地更新版本号（`mihomotui/version.go`）
2. 提交代码并推送
3. 打 tag 触发构建：`git tag v0.x.x && git push origin v0.x.x`
4. GitHub Actions 自动构建多架构二进制并发布到 Release 页面

## 支持的架构

| 架构 | 说明 |
|------|------|
| `amd64` | x86_64，主流服务器/PC |
| `arm64` | aarch64，ARM 服务器/树莓派 4 |
| `armv7` | ARMv7，树莓派 3/4（32位） |
| `386` | i386，旧设备兼容 |

## 许可证

[MIT](LICENSE)
