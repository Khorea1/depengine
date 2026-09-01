package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHomeDir expands a leading "~" to the user's home directory.
// Only a tilde at the start of the path (optionally followed by "/" or
// end-of-string) is expanded; a tilde anywhere else is left untouched.
// When the home directory cannot be determined, the input is returned
// unchanged so callers surface the original value.
func ExpandHomeDir(p string) string {
	switch {
	case p == "~":
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	case strings.HasPrefix(p, "~/"):
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~/"))
		}
		return p
	default:
		return p
	}
}