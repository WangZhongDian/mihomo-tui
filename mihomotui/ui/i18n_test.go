package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"mihomotui/mihomotui/i18n"
)

func TestSettingsPageRendersEnglishAndUsesStableFields(t *testing.T) {
	i18n.SetLanguage(i18n.English)
	t.Cleanup(func() { i18n.SetLanguage(i18n.SimplifiedChinese) })
	page := NewSettingsPage(tview.NewApplication())
	page.SetRect(0, 0, 150, 45)
	text := renderedText(page, 150, 45)
	for _, want := range []string{"System settings", "Language", "Follow system"} {
		if !strings.Contains(text, want) {
			t.Errorf("English settings missing %q:\n%s", want, text)
		}
	}
	pages := page.(*tview.Pages)
	main := pages.GetPage("main").(*tview.Flex)
	tabs := main.GetItem(0).(*tview.Flex)
	tabs.GetItem(1).(*tview.Button).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	text = renderedText(page, 150, 45)
	if !strings.Contains(text, "HTTP port") {
		t.Errorf("English mihomo settings missing HTTP port:\n%s", text)
	}
	if !isProxyPortField(fieldMixedPort) || isProxyPortField("Mixed port") {
		t.Fatal("proxy port behavior must depend on stable field IDs, not translated labels")
	}
}

func TestFirstRunLanguageSelectionRendersBothLanguages(t *testing.T) {
	page := newLanguageSetupPage(tview.NewApplication(), func() {})
	page.SetRect(0, 0, 100, 25)
	text := renderedText(page, 100, 25)
	for _, want := range []string{"Choose your language"} {
		if !strings.Contains(text, want) {
			t.Errorf("language setup missing %q:\n%s", want, text)
		}
	}
	compact := strings.ReplaceAll(text, " ", "")
	if !strings.Contains(compact, "简体中文") {
		t.Errorf("language setup missing Chinese option:\n%s", text)
	}
	root := page.(*tview.Pages)
	form := root.GetPage("language").(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
	dropdown := form.GetFormItem(0).(*tview.DropDown)
	if index, option := dropdown.GetCurrentOption(); (index != 0 && index != 1) || (option != "简体中文" && option != "English") {
		t.Errorf("unexpected default language option %q", option)
	}
}
