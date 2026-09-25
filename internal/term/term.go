// Package term measures terminal geometry for callers that must size their
// output. It reads nothing but explicit file handles, so callers choose which
// stream defines the available width.
package term

import "os"

// DefaultWidth is the width assumed when no terminal can be measured.
const DefaultWidth = 80

// OutputWidth returns the width of the terminal that defines the available
// output space: stdout first, because that is the stream being written, then
// stderr for the common case of a redirected stdout on an interactive
// terminal. It falls back to DefaultWidth when neither stream is a terminal.
func OutputWidth() int {
	return outputWidth(os.Stdout, os.Stderr)
}

func outputWidth(stdout, stderr *os.File) int {
	for _, f := range []*os.File{stdout, stderr} {
		if width := Width(f); width > 0 {
			return width
		}
	}
	return DefaultWidth
}
