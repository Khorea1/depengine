package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/state"
)

// Different project locks may describe different HTTP artifacts, but the
// second project must not silently relabel installed bytes from the first.
func TestFrozenMultiSchemaHTTPDoesNotRelabelWrongInstalledArtifact(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	actual := []byte("installed artifact A")
	if err := os.WriteFile(filepath.Join(root, "rust"), actual, 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	checksum := func(data []byte) string {
		hash := sha256.Sum256(data)
		return "sha256:" + hex.EncodeToString(hash[:])
	}
	dir := t.TempDir()
	writeSchema := func(name, url, digest string) string {
		path := filepath.Join(dir, name)
		data := fmt.Sprintf("schema_version = 1\n\n[tools.rust]\nmethod_only = [\"http\"]\nhttp = {url = %q, checksum = %q, extract_to = %q, binary = \"rust\", sudo_required = false}\n", url, digest, root)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	a := writeSchema("schema.toml", "https://example.invalid/rust-a", checksum(actual))
	b := writeSchema("secondary.toml", "https://example.invalid/rust-b", checksum([]byte("different artifact B")))
	for _, schema := range []string{a, b} {
		runTestUpdate(t, schema, "")
	}
	installFrozen := func(schema string) error {
		cmd := newInstallCmd()
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		for _, pair := range [][2]string{{"schema", schema}, {"no-manifest", "true"}, {"no-catalog", "true"}, {"frozen-lockfile", "true"}, {"quiet", "true"}} {
			if err := cmd.Flags().Set(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		return cmd.ExecuteContext(context.Background())
	}
	if err := installFrozen(a); err != nil {
		t.Fatalf("first schema matching existing artifact: %v", err)
	}
	previous, err := os.ReadFile(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := installFrozen(b); err == nil {
		t.Fatal("second schema reclassified different artifact as already installed")
	}
	after, err := os.ReadFile(state.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previous, after) {
		t.Fatal("failed second schema modified global installation state")
	}
}
