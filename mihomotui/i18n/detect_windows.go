package i18n

import (
	"os"
	"syscall"
)

var getUserDefaultUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// DetectSystemLanguage uses the Windows user UI language before locale variables.
func DetectSystemLanguage() string {
	if err := getUserDefaultUILanguage.Find(); err == nil {
		langID, _, _ := getUserDefaultUILanguage.Call()
		primary := uint16(langID) & 0x3ff
		switch primary {
		case 0x04:
			return SimplifiedChinese
		case 0x09:
			return English
		default:
			return English
		}
	}
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
