package i18n

import "testing"

func clearLocaleEnv(t *testing.T) {
	t.Helper()
	for _, env := range []string{"DEPENGINE_LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(env, "")
	}
}

func TestGetLocaleDefaultsToEnglish(t *testing.T) {
	clearLocaleEnv(t)
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en, got %s", got)
	}
}

// TestGetLocaleIgnoresHostLocale pins the opt-in contract: a pt-BR host must
// not get partially translated output just because of its locale.
func TestGetLocaleIgnoresHostLocale(t *testing.T) {
	clearLocaleEnv(t)
	t.Setenv("LANGUAGE", "pt_BR.UTF-8")
	t.Setenv("LC_ALL", "pt_BR.UTF-8")
	t.Setenv("LC_MESSAGES", "pt_BR.UTF-8")
	t.Setenv("LANG", "pt_BR.UTF-8")
	if got := GetLocale(); got != "en" {
		t.Fatalf("host locale must not enable localization; got %s", got)
	}
}

func TestGetLocaleDEPENGINE_LANG(t *testing.T) {
	for _, value := range []string{"pt", "pt-BR", "pt_BR.UTF-8"} {
		t.Run(value, func(t *testing.T) {
			clearLocaleEnv(t)
			t.Setenv("DEPENGINE_LANG", value)
			if got := GetLocale(); got != "pt" {
				t.Fatalf("DEPENGINE_LANG=%q: expected pt, got %s", value, got)
			}
		})
	}
}

func TestGetLocaleDEPENGINE_LANGAliasList(t *testing.T) {
	clearLocaleEnv(t)
	t.Setenv("DEPENGINE_LANG", "fr:pt")
	if got := GetLocale(); got != "pt" {
		t.Fatalf("expected pt from the alias list, got %s", got)
	}
}

func TestGetLocaleUnsupportedFallsBackToEnglish(t *testing.T) {
	clearLocaleEnv(t)
	t.Setenv("DEPENGINE_LANG", "fr_FR.UTF-8")
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en fallback, got %s", got)
	}
}

func TestGetLocaleDEPENGINE_LANGWinsOverLocale(t *testing.T) {
	clearLocaleEnv(t)
	t.Setenv("DEPENGINE_LANG", "en")
	t.Setenv("LANGUAGE", "pt_BR.UTF-8")
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en from DEPENGINE_LANG, got %s", got)
	}
}
