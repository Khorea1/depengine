package i18n

import (
	"testing"
)

func TestGetLocaleDefault(t *testing.T) {
	// No env vars set → should default to "en"
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(env, "")
	}
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en, got %s", got)
	}
}

func TestGetLocaleLANG(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en, got %s", got)
	}
}

func TestGetLocaleLCMessages(t *testing.T) {
	t.Setenv("LC_MESSAGES", "pt_BR.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8") // should be overridden
	if got := GetLocale(); got != "pt" {
		t.Fatalf("expected pt, got %s", got)
	}
}

func TestGetLocaleLCAll(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "pt_BR.UTF-8") // should be overridden
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en, got %s", got)
	}
}

func TestGetLocaleUnsupported(t *testing.T) {
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := GetLocale(); got != "en" {
		t.Fatalf("expected en (fallback), got %s", got)
	}
}

func TestGetLocalePTSet(t *testing.T) {
	t.Setenv("LANG", "pt_BR.UTF-8")
	if got := GetLocale(); got != "pt" {
		t.Fatalf("expected pt, got %s", got)
	}
}
