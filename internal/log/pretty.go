package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// prettyHandler is a human-oriented slog.Handler used for interactive CLI
// output (the default, non-JSON mode). It drops the timestamp and the raw
// "level=INFO" key/value noise of slog's built-in TextHandler in favor of a
// short, left-aligned, optionally-colored level tag — the same 2-space
// indented visual language as pkg/exec's ✓/✗/→ status lines, so structured
// log output ("no lockfile found — resolving latest versions") and per-tool
// status output don't read as two unrelated tools bolted together.
//
// Example:
//
//	INFO  no lockfile found — resolving latest versions
//	WARN  could not auto-resolve latest  error="rate limited" hint="run 'depengine update' manually"
//
// DEPENGINE_LOG_JSON=1 bypasses this entirely in favor of slog.JSONHandler
// (see New), which remains the contract for programmatic consumers.
type prettyHandler struct {
	mu    *sync.Mutex
	out   io.Writer
	level slog.Leveler
	color bool
	attrs []slog.Attr
	group string // dotted group-name prefix applied to subsequent attr keys
}

// newPrettyHandler creates a prettyHandler writing to out at the given
// level. Color is auto-detected: enabled only when out is a real terminal
// (os.Stderr/os.Stdout connected to a character device) and not disabled via
// NO_COLOR/TERM=dumb, matching the convention pkg/exec's status lines use.
func newPrettyHandler(out io.Writer, level slog.Leveler) *prettyHandler {
	return &prettyHandler{
		mu:    &sync.Mutex{},
		out:   out,
		level: level,
		color: shouldUseColor(out),
	}
}

// shouldUseColor reports whether ANSI color codes should be emitted for
// writer w. It mirrors pkg/exec's shouldUseColor convention (NO_COLOR,
// TERM=dumb, FORCE_COLOR, character-device check) so structured logs and
// tool-status lines make the same choice in the same process.
func shouldUseColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (h *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// levelTag returns the fixed-width level word and the ANSI color to wrap it
// in. The word itself ("INFO", "WARN", ...) is kept verbatim (not replaced
// by a bare symbol) so log output stays greppable and so existing tests that
// assert on the level word keep working.
func levelTag(level slog.Level) (word, ansi string) {
	switch {
	case level >= slog.LevelError:
		return "ERROR", "\033[31m" // red
	case level >= slog.LevelWarn:
		return "WARN", "\033[33m" // yellow
	case level >= slog.LevelInfo:
		return "INFO", "\033[36m" // cyan
	default:
		return "DEBUG", "\033[2m" // dim
	}
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	word, ansi := levelTag(r.Level)

	var b strings.Builder
	b.WriteString("  ")
	if h.color {
		b.WriteString(ansi)
		fmt.Fprintf(&b, "%-5s", word)
		b.WriteString("\033[0m")
	} else {
		fmt.Fprintf(&b, "%-5s", word)
	}
	b.WriteString("  ")
	b.WriteString(r.Message)

	// Multi-line values (a subprocess's captured stderr is the common case,
	// e.g. apt's two-line "Failed to fetch ... / repository is not signed")
	// are held back from the inline key=value stream and appended as an
	// indented block below the line instead. Jammed into the same line as
	// "\n"-escaped text, they read as unparseable noise; on their own
	// indented lines they read like the tool's own output.
	type block struct {
		key   string
		lines []string
	}
	var blocks []block

	writeAttr := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		s := a.Value.String()
		if strings.Contains(s, "\n") {
			blocks = append(blocks, block{key: key, lines: strings.Split(strings.TrimRight(s, "\n"), "\n")})
			return
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatAttrValue(a.Value))
	}
	for _, a := range h.attrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})
	b.WriteByte('\n')

	for _, blk := range blocks {
		fmt.Fprintf(&b, "         %s:\n", blk.key)
		for _, line := range blk.lines {
			b.WriteString("           ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

// formatAttrValue renders a slog value the way TextHandler does: quoted
// when it contains whitespace or is empty, bare otherwise. This keeps
// output greppable (tool=zsh, not tool="zsh") while staying unambiguous
// for values that need it (error="rate limited (60/h)").
func formatAttrValue(v slog.Value) string {
	s := v.String()
	if s == "" || strings.ContainsAny(s, " \t\n\"") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	newAttrs := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &prettyHandler{mu: h.mu, out: h.out, level: h.level, color: h.color, attrs: newAttrs, group: h.group}
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	group := name
	if h.group != "" {
		group = h.group + "." + name
	}
	return &prettyHandler{mu: h.mu, out: h.out, level: h.level, color: h.color, attrs: h.attrs, group: group}
}
