package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"depengine/pkg/schema"
)

// DefinitionHash computes a stable SHA256 hash of a tool's schema definition.
// The hash is based on the tool name and all its method candidates (kind + config),
// sorted by kind for stability regardless of declaration order.
func DefinitionHash(tool *schema.Tool) string {
	h := sha256.New()
	h.Write([]byte(tool.Name))
	h.Write([]byte{0})

	// Collect method keys and sort for reproducible output.
	types := make([]string, 0, len(tool.Methods))
	methodConfig := make(map[string]map[string]any, len(tool.Methods))
	for _, m := range tool.Methods {
		types = append(types, m.Kind)
		methodConfig[m.Kind] = m.Config
	}
	sort.Strings(types)

	enc := json.NewEncoder(h)
	for _, k := range types {
		_ = enc.Encode(k)
		_ = enc.Encode(methodConfig[k])
	}

	return hex.EncodeToString(h.Sum(nil))
}
