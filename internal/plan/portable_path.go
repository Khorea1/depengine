package plan

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidatePortablePathComponent rejects a path component that cannot be
// represented consistently on supported filesystems. Windows restrictions are
// enforced even when planning runs on Unix because portable plan/lock identity
// may later be consumed on Windows.
func ValidatePortablePathComponent(component string) error {
	if component == "" || component == "." || component == ".." {
		return fmt.Errorf("invalid portable path component %q", component)
	}
	if !utf8.ValidString(component) {
		return fmt.Errorf("path component is not valid UTF-8")
	}
	for _, r := range component {
		if r < 0x20 || strings.ContainsRune(`<>"|?*:`, r) {
			return fmt.Errorf("path component %q contains non-portable character %q", component, r)
		}
	}
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return fmt.Errorf("path component %q has a Windows-ambiguous trailing dot or space", component)
	}
	base := component
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("path component %q is a reserved Windows device name", component)
	}
	return nil
}
