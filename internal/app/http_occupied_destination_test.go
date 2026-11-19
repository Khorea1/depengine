package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Khorea1/depengine/internal/state"
)

func TestFrozenHTTPOccupiedBinaryDoesNotDownload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var requests atomic.Int32
	payload := []byte("test binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	root := t.TempDir()
	destination := filepath.Join(root, "rust")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	schema := filepath.Join(t.TempDir(), "schema.toml")
	contents := fmt.Sprintf("schema_version = 1\n[tools.rust]\nmethod_only = [\"http\"]\nhttp = {url = %q, checksum = %q, extract_to = %q, binary = \"rust\", sudo_required = false}\n", srv.URL+"/rust", "sha256:"+hex.EncodeToString(sum[:]), root)
	if err := os.WriteFile(schema, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestUpdate(t, schema, "")
	requests.Store(0)

	cmd := newInstallCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	for key, value := range map[string]string{"schema": schema, "no-catalog": "true", "no-manifest": "true", "frozen-lockfile": "true", "quiet": "true"} {
		if err := cmd.Flags().Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("install accepted a directory occupying the executable destination")
	}
	if requests.Load() != 0 {
		t.Fatalf("attempted %d downloads despite occupied destination", requests.Load())
	}
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		t.Fatalf("existing destination was modified: %v, %v", info, err)
	}
	current, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, tracked := current.Tools["rust"]; tracked {
		t.Fatal("failed observation was persisted as a global installation")
	}
}
