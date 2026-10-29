package ui

import "mihomotui/mihomotui/i18n"

// T returns a translated UI message.
func T(id string) string { return i18n.T(id) }

// Tf returns a translated message with named template values.
func Tf(id string, data any) string { return i18n.Tf(id, data) }
