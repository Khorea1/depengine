// Package i18n provides locale detection for the depengine CLI.
// It reads LANG/LC_MESSAGES/LC_ALL environment variables following
// POSIX conventions and returns the user's preferred language.
package i18n

import (
	"os"
	"strings"
)

// GetLocale returns the user's preferred language code.
// Returns "en" (English) as the default fallback.
// Currently supported: "pt", "en".
func GetLocale() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(env); v != "" {
			lang := strings.Split(v, ".")[0]
			lang = strings.Split(lang, "_")[0]
			switch lang {
			case "pt":
				return "pt"
			case "en":
				return "en"
			}
		}
	}
	return "en"
}
