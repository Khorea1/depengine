package main

import (
	"fmt"
	"os"
	"strings"
)

// ANSI SGR parameters used across the CLI. Kept as bare parameters (no
// "\x1b[" prefix) so helpers can compose them.
const (
	ansiBold   = "1"
	ansiDim    = "2"
	ansiRed    = "31"
	ansiGreen  = "32"
	ansiYellow = "33"
	ansiCyan   = "36"
)

// cliColor reports whether ANSI styling should be emitted to f. It honors
// NO_COLOR and TERM=dumb (off) and FORCE_COLOR (on), and otherwise requires
// f to be a character device — the same policy as pkg/exec's status lines,
// generalized to any stream so stdout-bound output (usage, why) and
// stderr-bound output (status, validate) each make the right call.
func cliColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// styled wraps s in the given SGR parameter when color is enabled, and
// returns s unchanged otherwise. Empty strings pass through untouched.
func styled(color bool, code, s string) string {
	if !color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// padRight pads s with trailing spaces to width bytes. All padded values in
// this package are ASCII (command names, status words), so byte length is
// fine; padding happens BEFORE colorization so ANSI escapes never throw off
// alignment — callers pad first, then styled().
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// printKV renders an aligned key/value block (two-space indent, dim keys)
// — the header style install/update/upgrade/graph share.
func printKV(c *cliStyle, title string, pairs ...[2]string) {
	fmt.Fprintln(c.w, c.bold(title))
	width := 0
	for _, kv := range pairs {
		if len(kv[0]) > width {
			width = len(kv[0])
		}
	}
	for _, kv := range pairs {
		fmt.Fprintf(c.w, "  %s  %s\n", c.dim(padRight(kv[0], width)), kv[1])
	}
	fmt.Fprintln(c.w)
}

// cliStyle bundles a writer with its cached color decision so per-line
// output doesn't re-stat the stream or re-read the environment.
type cliStyle struct {
	w     *os.File
	color bool
}

func newCLIStyle(w *os.File) *cliStyle { return &cliStyle{w: w, color: cliColor(w)} }

func (c *cliStyle) bold(s string) string  { return styled(c.color, ansiBold, s) }
func (c *cliStyle) dim(s string) string   { return styled(c.color, ansiDim, s) }
func (c *cliStyle) red(s string) string   { return styled(c.color, ansiRed, s) }
func (c *cliStyle) green(s string) string { return styled(c.color, ansiGreen, s) }
func (c *cliStyle) yellow(s string) string {
	return styled(c.color, ansiYellow, s)
}
func (c *cliStyle) cyan(s string) string { return styled(c.color, ansiCyan, s) }

// status renders one symbol-prefixed status line in the shared ✓/✗/–/→
// vocabulary of pkg/exec's status lines, with the symbol colored.
func (c *cliStyle) status(symbol, color, format string, args ...any) {
	line := fmt.Sprintf("  %s %s\n", styled(c.color, color, symbol), fmt.Sprintf(format, args...))
	fmt.Fprint(c.w, line)
}

func (c *cliStyle) ok(format string, args ...any)    { c.status("✓", ansiGreen, format, args...) }
func (c *cliStyle) fail(format string, args ...any)  { c.status("✗", ansiRed, format, args...) }
func (c *cliStyle) warn(format string, args ...any)  { c.status("⚠", ansiYellow, format, args...) }
func (c *cliStyle) skip(format string, args ...any)  { c.status("–", ansiYellow, format, args...) }
func (c *cliStyle) arrow(format string, args ...any) { c.status("→", ansiCyan, format, args...) }

// heading prints a section header ("Name (N):" style handled by the caller)
// in bold, preceded by a blank line when not first in the output.
func (c *cliStyle) heading(s string) { fmt.Fprintln(c.w, c.bold(s)) }

// plural returns "1 <s>" / "N <s>s" — keeps summary lines terse.
func plural(n int, s string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, s)
	}
	return fmt.Sprintf("%d %ss", n, s)
}

// joinCountParts renders a "3 installed · 1 outdated" footer, skipping
// zero counts.
func joinCountParts(parts []string) string {
	return strings.Join(parts, " · ")
}
