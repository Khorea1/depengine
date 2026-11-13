// Package i18n selects the language for depengine's command summaries.
//
// Localization is opt-in. Only DEPENGINE_LANG enables it: POSIX locale
// variables (LANGUAGE/LC_ALL/LC_MESSAGES/LANG) are deliberately ignored so a
// pt-BR host does not get partially translated output. Coverage today is the
// Short/Long/Example text of commands; flags, errors and logs stay English.
package i18n

import (
	"os"
	"strings"
)

// GetLocale returns the selected language code: "en" or "pt".
// Unset, empty, or unsupported values fall back to "en".
func GetLocale() string {
	for _, entry := range strings.Split(os.Getenv("DEPENGINE_LANG"), ":") {
		switch normalize(entry) {
		case "pt":
			return "pt"
		case "en":
			return "en"
		}
	}
	return "en"
}

// normalize reduces a locale spelling such as "pt_BR.UTF-8" to "pt".
func normalize(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	lang := strings.Split(entry, ".")[0]
	lang = strings.Split(lang, "_")[0]
	lang = strings.Split(lang, "-")[0]
	return lang
}
