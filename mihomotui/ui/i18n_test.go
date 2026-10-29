package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSettingsPageRendersEnglishAndUsesStableFields(t *testing.T) {
	useTestLanguage(t, "en")
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
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	page := newLanguageSetupPage(tview.NewApplication(), func() {})
	page.SetRect(0, 0, 100, 25)
	text := renderedText(page, 100, 25)
	if !strings.Contains(text, "Choose your language") {
		t.Errorf("language setup missing English prompt:\n%s", text)
	}
	root := page.(*tview.Pages)
	form := root.GetPage("language").(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
	dropdown := form.GetFormItem(0).(*tview.DropDown)
	if dropdown.GetOptionCount() != 2 {
		t.Fatalf("language option count = %d, want 2", dropdown.GetOptionCount())
	}
	for index, want := range []string{"简体中文", "English"} {
		dropdown.SetCurrentOption(index)
		_, option := dropdown.GetCurrentOption()
		if option != want {
			t.Errorf("language option %d = %q, want %q", index, option, want)
		}
	}
}
