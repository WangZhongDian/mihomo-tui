package ui

import (
	"testing"

	"mihomotui/mihomotui/i18n"
)

// useTestLanguage isolates UI tests from the process-wide localizer and the
// locale of the machine running the suite. Do not run such tests in parallel.
func useTestLanguage(t *testing.T, language string) {
	t.Helper()
	previous := i18n.Language()
	i18n.SetLanguage(language)
	t.Cleanup(func() { i18n.SetLanguage(previous) })
}
