package mihomotui

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"regexp"
)

func newSubscriptionID() string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return generateRandomSecret()
}

// RedactURL 保留定位订阅所需的信息，但不记录用户信息、查询参数或 fragment。
var embeddedHTTPURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

// RedactURLInText 移除文本中 HTTP(S) URL 的用户信息、查询参数和 fragment。
// 网络库返回的错误通常会回显原始 URL；在将错误保存到配置、输出到 UI 或日志前应使用该函数。
func RedactURLInText(text string) string {
	return embeddedHTTPURLPattern.ReplaceAllStringFunc(text, RedactURL)
}

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[invalid-url]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
