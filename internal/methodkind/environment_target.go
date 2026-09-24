package methodkind

import (
	"sort"

	"github.com/Khorea1/depengine/internal/plan"
)

// EnvironmentTargetContract maps adapter-neutral environment target kinds back
// to the adapter-facing config field consumed by legacy observe/version/remove
// boundaries. Methods that do not expose environment targeting leave this nil.
type EnvironmentTargetContract struct {
	ConfigFields map[plan.EnvironmentKind]string
}

// FieldFor returns the adapter config field for an environment target kind.
func (c *EnvironmentTargetContract) FieldFor(kind plan.EnvironmentKind) (string, bool) {
	if c == nil {
		return "", false
	}
	field, ok := c.ConfigFields[kind]
	return field, ok && field != ""
}

// Fields returns the declared adapter config fields without duplicates.
func (c *EnvironmentTargetContract) Fields() []string {
	if c == nil {
		return nil
	}
	seen := make(map[string]bool, len(c.ConfigFields))
	out := make([]string, 0, len(c.ConfigFields))
	for _, field := range c.ConfigFields {
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}
