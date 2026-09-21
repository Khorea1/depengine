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
	// It is a colon-separated list; each entry is tried in order.
	for _, env := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(env)
		if v == "" {
			continue
		}
		// Split by colon for LANGUAGE (priority list); other env vars
		// are single-valued per POSIX.
		entries := strings.Split(v, ":")
		if env != "LANGUAGE" {
			entries = entries[:1] // only first entry for non-LANGUAGE vars
		}
		for _, entry := range entries {
			if entry == "" {
				continue // skip empty entries
			}
			lang := strings.Split(entry, ".")[0]
			lang = strings.Split(lang, "_")[0]
			switch lang {
			case "pt":
				return "pt"
			case "en", "C", "POSIX":
				return "en"
			}
		}
	}
	return "en"
}
