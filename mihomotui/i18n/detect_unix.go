//go:build !windows

package i18n

import "os"

// DetectSystemLanguage returns a supported UI language, defaulting to English.
func DetectSystemLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(key); value != "" {
			tag, ok := NormalizeLanguage(value)
			if ok && tag != FollowSystem {
				return tag
			}
			return English
		}
	}
	return English
}
