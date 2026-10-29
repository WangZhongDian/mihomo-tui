package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"text/template"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

func TestSystemLanguageDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix locale priority is covered by platform-specific detection tests")
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	if got := DetectSystemLanguage(); got != English {
		t.Fatalf("empty locale = %q, want English", got)
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := DetectSystemLanguage(); got != SimplifiedChinese {
		t.Fatalf("Chinese locale = %q, want zh-CN", got)
	}
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := DetectSystemLanguage(); got != English {
		t.Fatalf("unsupported locale = %q, want English", got)
	}
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	if got := DetectSystemLanguage(); got != English {
		t.Fatalf("English locale = %q, want en", got)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	for _, value := range []string{"zh", "zh_CN.UTF-8", "zh-Hans-CN", "zh-SG"} {
		if got, ok := NormalizeLanguage(value); !ok || got != SimplifiedChinese {
			t.Errorf("NormalizeLanguage(%q) = %q, %v", value, got, ok)
		}
	}
	for _, value := range []string{"C", "POSIX", "ja-JP", "invalid"} {
		if got, ok := NormalizeLanguage(value); ok || got != English {
			t.Errorf("NormalizeLanguage(%q) = %q, %v, want English fallback", value, got, ok)
		}
	}
	if got, ok := NormalizeLanguage(""); !ok || got != FollowSystem {
		t.Errorf("empty preference = %q, %v, want follow system", got, ok)
	}
}

func TestTranslationFallbackAndTemplates(t *testing.T) {
	SetLanguage(SimplifiedChinese)
	t.Cleanup(func() { SetLanguage(English) })
	if got := T("nav.home"); got != "首页" {
		t.Fatalf("Chinese translation = %q", got)
	}
	if got := Tf("settings.about.version", map[string]any{"Version": "1.2.3"}); got != "版本: 1.2.3" {
		t.Fatalf("Chinese template = %q", got)
	}
	fallback := goi18n.NewBundle(language.English)
	fallback.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	if _, err := fallback.ParseMessageFileBytes([]byte("nav.home: Home\n"), "en.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := fallback.ParseMessageFileBytes([]byte("nav.proxy: 代理\n"), "zh-CN.yaml"); err != nil {
		t.Fatal(err)
	}
	localizer := goi18n.NewLocalizer(fallback, SimplifiedChinese)
	if got, _ := localizer.Localize(&goi18n.LocalizeConfig{MessageID: "nav.home"}); got != "Home" {
		t.Fatalf("missing Chinese translation did not fall back to English: %q", got)
	}
	SetLanguage(English)
	if got := T("nav.home"); got != "Dashboard" {
		t.Fatalf("English translation = %q", got)
	}
	if got := T("test.missing.message"); got != "test.missing.message" {
		t.Fatalf("missing ID should remain visible: %q", got)
	}
}

func TestCatalogsHaveMatchingKeysAndTemplates(t *testing.T) {
	entries, err := fs.ReadDir(localeFiles, "locales")
	if err != nil {
		t.Fatal(err)
	}
	catalogs := map[string]map[string]any{}
	for _, entry := range entries {
		data, err := localeFiles.ReadFile("locales/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		var values map[string]any
		if err := yaml.Unmarshal(data, &values); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for id, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Errorf("%s: %s is not a string", entry.Name(), id)
				continue
			}
			if _, err := template.New(id).Parse(text); err != nil {
				t.Errorf("%s: %s: invalid template: %v", entry.Name(), id, err)
			}
		}
		catalogs[entry.Name()] = values
	}
	for id, value := range catalogs["en.yaml"] {
		other, ok := catalogs["zh-CN.yaml"][id]
		if !ok {
			t.Errorf("English message %q missing from zh-CN", id)
			continue
		}
		if templateFields(value.(string)) != templateFields(other.(string)) {
			t.Errorf("message %q has different template fields", id)
		}
	}
	for id := range catalogs["zh-CN.yaml"] {
		if _, ok := catalogs["en.yaml"][id]; !ok {
			t.Errorf("Chinese message %q missing from English", id)
		}
	}
}

func TestAllReferencedMessagesExist(t *testing.T) {
	files, err := filepath.Glob("../ui/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]string
	data, err := localeFiles.ReadFile("locales/en.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\bTf?\("([^"]+)"`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
			if _, ok := catalog[match[1]]; !ok {
				t.Errorf("%s references missing message %q", file, match[1])
			}
		}
	}
}

var templateFieldPattern = regexp.MustCompile(`\{\{[^}]*?\.([A-Za-z][A-Za-z0-9_]*)[^}]*?\}\}`)

func templateFields(value string) string {
	return strings.Join(templateFieldPattern.FindAllString(value, -1), ",")
}
