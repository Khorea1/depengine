package i18n

// Package i18n provides locale detection for the depengine CLI.
// It reads LANGUAGE/LC_ALL/LC_MESSAGES/LANG environment variables following
// POSIX and GNU gettext conventions, returning the user's preferred language.

import (
	"os"
	"strings"
)

// GetLocale returns the user's preferred language code.
// Returns "en" (English) as the default fallback.
// Currently supported: "pt", "en".
func GetLocale() string {
	// LANGUAGE (GNU gettext) has highest priority for message language.
	// It is a colon-separated list; we check the first entry.
	for _, env := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(env); v != "" {
			// LANGUAGE is colon-separated; take the first entry.
			lang := strings.Split(v, ":")[0]
			lang = strings.Split(lang, ".")[0]
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
