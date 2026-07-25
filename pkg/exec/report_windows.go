//go:build windows

package exec

// terminalWidth returns the width of the terminal connected to stderr.
// On Windows this always returns 0.
func terminalWidth() int {
	return 0
}