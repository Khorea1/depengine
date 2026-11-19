package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/lock"
)

func TestUpdateWaitsForLockTransactionBeforeReadingAndWriting(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.toml")
	checksum := "sha256:" + strings.Repeat("a", 64)
	if err := os.WriteFile(schema, []byte("schema_version = 1\n[tools.example.http]\nurl = \"https://example.org/example\"\nchecksum = \""+checksum+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := lock.DefaultPath(schema)
	guard, err := lock.AcquireTransaction(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Millisecond)
	defer cancel()
	noManifest, frozen, dryRun, verbose, quiet, jsonMode := true, false, false, false, true, false
	manifest, override, profile := "", "", ""
	err = runUpdate(ctx, &schema, &manifest, &noManifest, &override, &profile, &frozen, &dryRun, &verbose, &quiet, &jsonMode, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("update with held lock = %v, want context deadline exceeded", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("update touched lock without transaction: stat = %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runUpdate(context.Background(), &schema, &manifest, &noManifest, &override, &profile, &frozen, &dryRun, &verbose, &quiet, &jsonMode, nil); err != nil {
		t.Fatalf("update after lock released: %v", err)
	}
	if _, err := lock.Load(lockPath); err != nil {
		t.Fatal(err)
	}
}

func TestInstallWaitsForLockTransactionBeforeExecuting(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(schema, []byte("schema_version = 1\n[tools.example.http]\nurl = \"https://example.org/example\"\nchecksum = \"sha256:"+strings.Repeat("a", 64)+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := lock.AcquireTransaction(context.Background(), lock.DefaultPath(schema))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.Close() }()
	cmd := newInstallCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, flag := range [][2]string{{"schema", schema}, {"no-manifest", "true"}} {
		if err := cmd.Flags().Set(flag[0], flag[1]); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Millisecond)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("install with held lock = %v, want context deadline exceeded", err)
	}
	if _, err := os.Stat(lock.DefaultPath(schema)); !os.IsNotExist(err) {
		t.Fatalf("install wrote lock while transaction blocked: stat = %v", err)
	}
}
