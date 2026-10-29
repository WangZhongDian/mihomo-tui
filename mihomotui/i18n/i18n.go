// Package i18n provides the application's embedded, user-facing translations.
package i18n

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

const (
	English           = "en"
	SimplifiedChinese = "zh-CN"
	FollowSystem      = ""
)

//go:embed locales/*.yaml
var localeFiles embed.FS

var (
	bundle     *goi18n.Bundle
	current    *goi18n.Localizer
	currentTag = English
	mu         sync.RWMutex
)

func init() {
	bundle = goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	for _, name := range []string{"en", "zh-CN"} {
		path := "locales/" + name + ".yaml"
		if _, err := bundle.LoadMessageFileFS(fs.FS(localeFiles), path); err != nil {
			panic(fmt.Errorf("load embedded translations %s: %w", path, err))
		}
	}
	current = goi18n.NewLocalizer(bundle, English)
}

// NormalizeLanguage maps supported user preferences to a canonical language tag.
// Empty values mean "follow the operating system" and are intentionally preserved.
func NormalizeLanguage(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" {
		return FollowSystem, true
	}
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		value = value[:dot]
	}
	if at := strings.IndexByte(value, '@'); at >= 0 {
		value = value[:at]
	}
	lower := strings.ToLower(value)
	switch {
	case lower == "en" || strings.HasPrefix(lower, "en-"):
		return English, true
	case lower == "zh" || strings.HasPrefix(lower, "zh-hans") || strings.HasPrefix(lower, "zh-cn") || strings.HasPrefix(lower, "zh-sg"):
		return SimplifiedChinese, true
	default:
		return English, false
	}
}

// SetLanguage changes the process-wide localizer. Call it only on the UI thread.
func SetLanguage(value string) {
	tag, ok := NormalizeLanguage(value)
	if !ok || tag == FollowSystem {
		tag = English
	}
	localizer := goi18n.NewLocalizer(bundle, tag, English)
	mu.Lock()
	current, currentTag = localizer, tag
	mu.Unlock()
}

// Language returns the active canonical language tag.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentTag
}

// T translates a stable message ID. Missing translations fall back to English;
// missing IDs remain visible for diagnosis instead of blanking out a UI control.
func T(id string) string {
	return Tf(id, nil)
}

// Tf translates a message with text/template data.
func Tf(id string, data any) string {
	mu.RLock()
	localizer := current
	mu.RUnlock()
	text, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: id, TemplateData: data})
	if text != "" {
		return text
	}
	if err != nil {
		slog.Warn("translation missing or invalid", "id", id, "error", err)
	}
	return id
}
