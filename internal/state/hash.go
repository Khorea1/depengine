package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
)

// definitionHash computes a stable SHA256 hash of a tool's schema definition.
// The hash covers the tool name, requires, preinstall, postinstall, tags,
// and every method's kind, config, and when condition. Methods are sorted
// by (kind, intra-kind ordinal) for reproducible output even with duplicate
// kinds.
func definitionHash(tool *config.Tool) string {
	h := sha256.New()
	h.Write([]byte(tool.Name))
	h.Write([]byte{0})

	// Include Requires.
	for _, req := range tool.Requires {
		h.Write([]byte(req))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})
	// RequiresWhen: hash each gated dep with its condition.
	deps := make([]string, 0, len(tool.RequiresWhen))
	for dep := range tool.RequiresWhen {
		deps = append(deps, dep)
	}
	sort.Strings(deps)
	for _, dep := range deps {
		h.Write([]byte(dep))
		h.Write([]byte{0})
		h.Write([]byte(fmt.Sprintf("%v", tool.RequiresWhen[dep])))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})

	writeHooks(h, tool.PreInstall)
	writeHooks(h, tool.PostInstall)

	// Include Tags.
	for _, tag := range tool.Tags {
		h.Write([]byte(tag))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})

	// Collect all method entries, assigning an intra-kind ordinal so that
	// duplicate kinds are distinguishable without making the hash depend
	// on declaration order for non-duplicate entries.
	type methodEntry struct {
		kind        string
		idx         int
		config      map[string]any
		when        *config.Condition
		preInstall  []config.Hook
		postInstall []config.Hook
	}
	kindCount := map[string]int{}
	entries := make([]methodEntry, 0, len(tool.Methods))
	for _, m := range tool.Methods {
		idx := kindCount[m.Kind]
		kindCount[m.Kind] = idx + 1
		entries = append(entries, methodEntry{
			kind:        m.Kind,
			idx:         idx,
			config:      m.Config,
			when:        m.When,
			preInstall:  m.PreInstall,
			postInstall: m.PostInstall,
		})
	}

	// Sort by kind first, then intra-kind ordinal.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
		}
		return entries[i].idx < entries[j].idx
	})

	for _, e := range entries {
		_, _ = h.Write([]byte(e.kind))
		h.Write([]byte{0})
		_, _ = h.Write([]byte(fmt.Sprintf("%d", e.idx)))
		h.Write([]byte{0})
		writeMapCanonical(h, e.config)
		if e.when != nil {
			_, _ = h.Write([]byte(strings.Join(e.when.DistroFamily, "\x00")))
		}
		h.Write([]byte{0})
		// Candidate-local hooks are part of the full definition/audit hash, but
		// only add bytes when present so definitions that predate this surface
		// keep their historical hash.
		if len(e.preInstall) > 0 || len(e.postInstall) > 0 {
			writeHooks(h, e.preInstall)
			writeHooks(h, e.postInstall)
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// DefinitionHash computes the complete schema-definition fingerprint, including
// one-shot lifecycle hooks. It is useful for provenance/audit, but it is not a
// health signal: a hook having changed or run does not describe current host
// state.
func DefinitionHash(tool *config.Tool) string {
	return definitionHash(tool)
}

// DesiredStateHash fingerprints the same normalized definition while excluding
// one-shot lifecycle hooks. Status uses this hash for schema drift so health is
// never inferred from whether a hook succeeded on an earlier transition.
func DesiredStateHash(tool *config.Tool) string {
	if tool == nil {
		return ""
	}
	copyTool := *tool
	copyTool.PreInstall = nil
	copyTool.PostInstall = nil
	copyTool.Methods = append([]*config.MethodCandidate(nil), tool.Methods...)
	for i, method := range copyTool.Methods {
		if method == nil {
			continue
		}
		methodCopy := *method
		methodCopy.PreInstall = nil
		methodCopy.PostInstall = nil
		copyTool.Methods[i] = &methodCopy
	}
	return definitionHash(&copyTool)
}

func writeHooks(h hash.Hash, hooks []config.Hook) {
	for _, hook := range hooks {
		for _, arg := range hook.Run {
			h.Write([]byte(arg))
			h.Write([]byte{0})
		}
		if hook.When != nil {
			h.Write([]byte(fmt.Sprintf("%v", hook.When)))
		}
		h.Write([]byte{0})
	}
	h.Write([]byte{0})
}

// VersionOutdated reports whether the installed version differs from the
// pinned version — the version-drift half of outdated detection (the other
// half is DefinitionHash). A leading "v"/"V" and surrounding whitespace are
// ignored, and dot-separated segments are compared numerically ("1.2" equals
// "1.2.0"). It returns false when either version is unknown (empty), since
// drift cannot be proven without both sides.
func VersionOutdated(installed, pinned string) bool {
	if installed == "" || pinned == "" {
		return false
	}
	return compareVersions(installed, pinned) != 0
}

// compareVersions compares two version strings segment by segment, treating
// segments as numbers when both parse, and falling back to string comparison
// otherwise (covers pre-release suffixes like "1.2.3-rc1" and date tags).
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := range n {
		sa, sb := "0", "0"
		if i < len(pa) {
			sa = pa[i]
		}
		if i < len(pb) {
			sb = pb[i]
		}
		na, ea := strconv.ParseInt(sa, 10, 64)
		nb, eb := strconv.ParseInt(sb, 10, 64)
		if ea == nil && eb == nil {
			switch {
			case na < nb:
				return -1
			case na > nb:
				return 1
			}
			continue
		}
		switch {
		case sa < sb:
			return -1
		case sa > sb:
			return 1
		}
	}
	return 0
}

// versionParts splits a version string into dot-separated segments, dropping
// a leading "v"/"V" prefix and surrounding whitespace.
func versionParts(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return strings.Split(v, ".")
}

// writeMapCanonical writes a deterministic hash of a map[string]any by
// sorting keys lexicographically and recursing into nested maps and slices.
func writeMapCanonical(h hash.Hash, m map[string]any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		h.Write([]byte{0})
		writeValueCanonical(h, m[k])
		h.Write([]byte{0})
	}
}
func writeValueCanonical(h hash.Hash, v any) {
	switch val := v.(type) {
	case nil:
		_, _ = h.Write([]byte("nil"))
	case string:
		_, _ = h.Write([]byte(val))
	case bool:
		_, _ = h.Write([]byte(fmt.Sprintf("%t", val)))
	case float64:
		_, _ = h.Write([]byte(fmt.Sprintf("%v", val)))
	case map[string]any:
		writeMapCanonical(h, val)
	case []any:
		for _, elem := range val {
			writeValueCanonical(h, elem)
			h.Write([]byte{0})
		}
	default:
		_, _ = h.Write([]byte(fmt.Sprintf("%v", val)))
	}
}
