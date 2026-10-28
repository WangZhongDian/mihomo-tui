package cmd

// ServiceUnitTemplate systemd 服务文件模板
// 占位符: {{.ExecPath}} — 可执行文件路径, {{.User}} — 运行用户
//
// 注意 ExecStop 只清理 TUN 路由规则（cleanup --tun-only），
// 不动用户 shell 里的系统代理注入块：stop/restart 属于常规运维动作，
// 升级脚本每次都会 stop，若在这里清 shell 会让用户的系统代理反复失效。
const ServiceUnitTemplate = `[Unit]
Description=mihomo-tui daemon
After=network.target

[Service]
Type=simple
User={{.User}}
UMask=0077
RuntimeDirectory=mihomo-tui
RuntimeDirectoryMode=0750
# root daemon 会将 socket 设为 root:mihomo-tui 0660；mihomo-tui 为只读组，mihomo-tui-operator 可管理订阅。
ExecStart={{.ExecPath}} server
ExecStop={{.ExecPath}} cleanup --tun-only
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
