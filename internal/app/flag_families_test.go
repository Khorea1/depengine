package app

import "testing"

// TestCommandFlagFamilies pins the flag contract across command families.
// Divergence is a usability bug: the same flag should not disappear or change
// name depending on the verb. Documented exceptions are encoded here rather
// than left to prose.
func TestCommandFlagFamilies(t *testing.T) {
	setEnglishLocale(t)
	docs := normalizeCLI(newRootCmd())

	// Every project command accepts an explicit schema.
	requireFlags(t, docs, []string{
		"depengine audit", "depengine check", "depengine doctor", "depengine graph", "depengine install",
		"depengine remove", "depengine status", "depengine update", "depengine upgrade",
		"depengine validate", "depengine why",
	}, "schema")

	// Layer selection is uniform. remove is intentionally state-first: it uses
	// the schema only for legacy v1 pin compatibility and never merges the
	// manifest or catalog, so it is excluded here.
	requireFlags(t, docs, []string{
		"depengine audit", "depengine check", "depengine doctor", "depengine graph", "depengine install",
		"depengine status", "depengine update", "depengine upgrade",
		"depengine validate", "depengine why",
	}, "manifest", "no-manifest", "catalog", "no-catalog")

	// Commands that emit progress accept --quiet.
	requireFlags(t, docs, []string{
		"depengine install", "depengine remove", "depengine status",
		"depengine update", "depengine upgrade",
	}, "quiet")

	// Only commands with a confirmation prompt accept --force.
	requireFlags(t, docs, []string{"depengine remove", "depengine upgrade"}, "force")

	// Tool selectors keep their names and semantics across commands.
	requireFlags(t, docs, []string{
		"depengine graph", "depengine install", "depengine remove", "depengine upgrade",
	}, "only")
	requireFlags(t, docs, []string{
		"depengine graph", "depengine install", "depengine upgrade",
	}, "skip")
	requireFlags(t, docs, []string{
		"depengine graph", "depengine install", "depengine update",
	}, "profile")
}

func requireFlags(t *testing.T, docs []cliCommandDoc, commands []string, flags ...string) {
	t.Helper()
	for _, path := range commands {
		doc := commandDocByPath(docs, path)
		if doc == nil {
			t.Errorf("missing command %q", path)
			continue
		}
		have := make(map[string]bool, len(doc.Flags))
		for _, flag := range doc.Flags {
			have[flag.Name] = true
		}
		for _, flag := range flags {
			if !have[flag] {
				t.Errorf("%s is missing --%s", path, flag)
			}
		}
	}
}
