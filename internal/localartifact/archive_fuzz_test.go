package localartifact

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzSafeArchiveTargetNeverEscapesRoot(f *testing.F) {
	for _, seed := range []string{
		"bin/tool",
		"../escape",
		"/absolute",
		"C:/escape",
		`dir\tool`,
		"a/./b",
		"a//b",
		"NUL",
		"dir/file?name",
		"./",
	} {
		f.Add(seed)
	}
	root := filepath.Join(string(filepath.Separator), "tmp", "depengine-archive-root")
	f.Fuzz(func(t *testing.T, name string) {
		target, err := safeArchiveTarget(root, name)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			t.Fatalf("Rel(%q, %q): %v", root, target, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("safeArchiveTarget escaped root: name=%q target=%q rel=%q", name, target, rel)
		}
		if strings.Contains(name, `\`) {
			t.Fatalf("safeArchiveTarget accepted backslash-bearing name %q", name)
		}
	})
}
