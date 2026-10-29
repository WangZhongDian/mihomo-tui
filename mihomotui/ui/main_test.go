package ui

import (
	"os"
	"testing"

	"mihomotui/mihomotui/i18n"
)

func TestMain(m *testing.M) {
	// Preserve the language assumed by the pre-i18n rendering tests.
	i18n.SetLanguage(i18n.SimplifiedChinese)
	os.Exit(m.Run())
}
