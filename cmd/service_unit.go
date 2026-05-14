package cmd

// ServiceUnitTemplate systemd 服务文件模板
// 占位符: {{.ExecPath}} — 可执行文件路径, {{.User}} — 运行用户
const ServiceUnitTemplate = `[Unit]
Description=mihomo-tui daemon
After=network.target

[Service]
Type=simple
User={{.User}}
ExecStart={{.ExecPath}} server
ExecStop={{.ExecPath}} cleanup
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
