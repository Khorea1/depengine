package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"depengine/pkg/schema"
)

// DefinitionHash computes a stable SHA256 hash of a tool's schema definition.
// The hash covers the tool name, requires, postinstall, and every method's
// kind, config, and when condition. Methods are sorted by (kind, intra-kind
// ordinal) for reproducible output even with duplicate kinds.
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

	// Include PostInstall.
	h.Write([]byte(tool.PostInstall))
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

	enc := json.NewEncoder(h)
	for _, e := range entries {
		_ = enc.Encode(e.kind)
		_ = enc.Encode(e.idx)
		_ = enc.Encode(e.config)
		if e.when != nil {
			_ = enc.Encode(e.when.DistroFamily)
		} else {
			_ = enc.Encode(nil)
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}
