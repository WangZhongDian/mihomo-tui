# mihomo-tui

mihomo-tui 是 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash Meta）的终端 UI 与守护进程管理工具。基于 [rivo/tview](https://github.com/rivo/tview) 开发，面向 Linux 无桌面服务器环境：TUI 通过 IPC 连接守护进程，安全管理 mihomo 进程、订阅、规则、内核版本与网络能力。

[English README](README.md)

## 功能特性

- **代理管理** — 可视化节点列表，支持按延迟排序、多选测速、自动选择最优节点
- **订阅池与主动接管** — URL、本地文件、粘贴内容和标准输入导入；daemon 主动校验、缓存订阅，保留最后可用版本并支持离线启动
- **高可用订阅池** — 支持顺序主备自动故障切换及合并模式；展示活动源、缓存、健康状态、失败次数与切换原因
- **订阅额度信息** — 解析常见 `subscription-userinfo` / CDN 前缀响应头、到期时间与运行期 provider 元数据
- **规则管理** — 查看当前生效规则；内置规则可启用/禁用、编辑、排序、恢复默认；自定义规则支持前置或后置
- **内核与资源管理** — 缓存 Release 版本列表，支持多版本下载、切换、删除和进度展示；可管理 GeoIP / GeoSite 下载地址
- **连接监控** — 实时查看活跃连接、流量统计与连接详情
- **延迟测试** — 批量节点延迟测试，可视化结果排序
- **系统代理** — 一键开启/关闭系统代理（HTTP/SOCKS5）
- **TUN 模式** — 支持 TUN 模式与路由配置
- **日志查看** — 实时滚动日志，支持过滤与暂停
- **systemd 服务** — 内置服务安装/卸载，开机自启支持
- **鼠标支持** — 支持鼠标点击操作按钮、切换页面和选择节点
- **动态分页** — 终端尺寸变化时自动适配表单和代理列表分页
- **TUN Docker 兼容** — 修复 TUN 模式下 Docker 容器回包路由问题，宿主机端口可被容器正常访问

## 界面预览

| 代理页面 | 规则页面 | 资源管理 |
|:--------:|:--------:|:--------:|
| ![代理页面](docs/proxy.png) | ![规则页面](docs/rules.png) | ![资源管理](docs/mihomo_manager.png) |

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

```text
mihomo-tui — mihomo 终端 UI 与守护进程管理工具

用法:
  mihomo-tui [选项]                              启动 TUI 客户端
  mihomo-tui server [-d <目录>]                  启动后台 IPC 服务
  mihomo-tui subscription import <导入选项>      导入并由 daemon 主动接管订阅
  mihomo-tui install_service                     安装为 systemd 服务（需 root）
  mihomo-tui uninstall                           卸载 systemd 服务（需 root）
  mihomo-tui grant_operator <用户名>             授予普通用户 IPC 管理权限（需 root）
  mihomo-tui cleanup                             清理系统代理和 TUN 环境（需 root）
  mihomo-tui tun_diagnose                        输出 TUN 路由 dry-run 计划（不修改系统）
  mihomo-tui version                             显示版本信息
  mihomo-tui help                                显示帮助

全局选项（放在子命令前）:
  -d <目录>          指定配置目录
  -standalone         启动嵌入式 IPC 服务（一体模式；仅 TUI）

subscription import 导入选项（必须且只能选择一种来源）:
  --url <URL>         导入远程订阅 URL
  --file <路径>       读取本地订阅文件并导入内容
  --stdin             从标准输入读取订阅内容
  --name <名称>       可选的订阅显示名称
  --via-local-proxy   后续刷新远程 URL 时通过本地 mihomo HTTP 代理
```

示例：

```bash
mihomo-tui --standalone
mihomo-tui subscription import --url 'https://example.com/sub?token=***' --name 我的订阅
mihomo-tui subscription import --file ./subscription.yaml
cat subscription.txt | mihomo-tui subscription import --stdin --name 离线订阅
sudo mihomo-tui grant_operator <用户名>
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
