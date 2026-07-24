package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strings"

	"depengine/pkg/schema"
)

// DefinitionHash computes a stable SHA256 hash of a tool's schema definition.
// The hash covers the tool name, requires, preinstall, postinstall, tags,
// and every method's kind, config, and when condition. Methods are sorted
// by (kind, intra-kind ordinal) for reproducible output even with duplicate
// kinds.
func DefinitionHash(tool *schema.Tool) string {
	h := sha256.New()
	h.Write([]byte(tool.Name))
	h.Write([]byte{0})

	// Include Requires.
	for _, req := range tool.Requires {
		h.Write([]byte(req))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})

	// Include PreInstall.
	h.Write([]byte(tool.PreInstall))
	h.Write([]byte{0})

	// Include PostInstall.
	h.Write([]byte(tool.PostInstall))
	h.Write([]byte{0})

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
		kind   string
		idx    int
		config map[string]any
		when   *schema.Condition
	}
	kindCount := map[string]int{}
	entries := make([]methodEntry, 0, len(tool.Methods))
	for _, m := range tool.Methods {
		idx := kindCount[m.Kind]
		kindCount[m.Kind] = idx + 1
		entries = append(entries, methodEntry{
			kind:   m.Kind,
			idx:    idx,
			config: m.Config,
			when:   m.When,
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
	}

	return hex.EncodeToString(h.Sum(nil))
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
