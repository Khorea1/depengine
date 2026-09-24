//go:build windows

package term

import "os"

// Width returns the width of the terminal attached to f.
// Terminal sizing is not implemented on Windows; the result is always 0,
// which makes callers fall back to their default width.
func Width(_ *os.File) int {
	return 0
}
