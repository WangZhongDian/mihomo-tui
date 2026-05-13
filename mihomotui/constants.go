package mihomotui

import "time"

// 默认端口
const (
	DefaultHTTPPort   = 7890
	DefaultSOCKS5Port = 7891
	DefaultMixedPort  = 7892
	DefaultRedirPort  = 7893
	DefaultTProxyPort = 7894
)

// 文件与目录名
const (
	ConfigDirName    = "mihomo-tui"
	ConfigFileName   = "config.yaml"
	MihomoConfigName = "config.yaml"
	SubscriptionsDir = "subscriptions"
)

// 时间与间隔
const (
	TimeFormatShort          = "2006-01-02 15:04"
	TimeFormatLog            = "2006-01-02 15:04:05"
	DayInSeconds             = 86400
	HealthCheckInterval      = 300
	ProgressBarWidth         = 20
	DefaultRefreshInterval   = 5 * time.Second
	DefaultStreamInterval    = 200 * time.Millisecond
	DefaultDaemonWaitTimeout = 5 * time.Second
	DefaultIPCRequestTimeout = 30 * time.Second
)

// Socket 路径
const (
	SocketDir  = "/var/run/mihomo-tui"
	SocketFile = "daemon.sock"
)

// 缓冲区大小
const (
	DownloadBufferSize = 32 * 1024
)

// 延迟状态常量
const (
	DelayUntested = -3 // 未测试
	DelayTesting  = -2 // 测试中
	DelayTimeout  = -1 // 超时/失败
)

// 颜色常量（tview 动态颜色标签）
const (
	ColorOK     = "green"
	ColorWarn   = "yellow"
	ColorError  = "red"
	ColorMuted  = "gray"
	ColorInfo   = "blue"
	ColorHeader = "blue::b"
)

// 下载 URL
const (
	DefaultMihomoDownloadURL  = "https://github.com/MetaCubeX/mihomo/latest/download/"
	DefaultGeoIPDownloadURL   = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip-lite.dat"
	DefaultGeoSiteDownloadURL = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
)
