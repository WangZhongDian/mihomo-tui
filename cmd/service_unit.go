package cmd

// ServiceUnitTemplate systemd 服务文件模板
// 占位符: {{.ExecPath}} — 可执行文件路径, {{.User}} — 运行用户
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
ExecStop={{.ExecPath}} cleanup
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
