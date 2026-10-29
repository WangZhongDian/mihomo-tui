package mihomotui

import (
	"os"
	"path/filepath"
	"testing"

	"mihomotui/mihomotui/i18n"
)

func TestFirstInstallLanguageSetupRunsOnce(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)
	if !NeedsLanguageSetup() {
		t.Fatal("empty user directory should trigger first-run language setup")
	}
	if err := SetLanguagePreference(i18n.SimplifiedChinese); err != nil {
		t.Fatal(err)
	}
	if NeedsLanguageSetup() {
		t.Fatal("initialized user should not see setup again")
	}
	resetClientPreferencesForTest()
	if got := EffectiveLanguage(); got != i18n.SimplifiedChinese {
		t.Fatalf("persisted language = %q", got)
	}
}

func TestExistingUserFollowsSystemWithoutPrompt(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)
	if err := os.WriteFile(filepath.Join(GetConfigDir(), ConfigFileName), []byte("system:\n  language: zh-CN\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if NeedsLanguageSetup() {
		t.Fatal("existing installation must not prompt again")
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got := EffectiveLanguage(); got != i18n.English {
		t.Fatalf("unsupported system language = %q, want English", got)
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := EffectiveLanguage(); got != i18n.SimplifiedChinese {
		t.Fatalf("Chinese system language = %q", got)
	}
}

func TestExplicitPreferenceOverridesSystemAndFollowSystemCanBeRestored(t *testing.T) {
	useTestConfigDir(t)
	resetClientPreferencesForTest()
	t.Cleanup(resetClientPreferencesForTest)
	t.Setenv("LANG", "en_US.UTF-8")
	if err := SetLanguagePreference(i18n.SimplifiedChinese); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveLanguage(); got != i18n.SimplifiedChinese {
		t.Fatalf("explicit preference = %q", got)
	}
	if err := SetFollowSystemPreference(); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveLanguage(); got != i18n.English {
		t.Fatalf("follow-system preference = %q", got)
	}
	if NeedsLanguageSetup() {
		t.Fatal("follow-system is an initialized preference")
	}
}
