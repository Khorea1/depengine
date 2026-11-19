package lock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This helper runs in a DIFFERENT process so the lock tests cannot pass due
// merely to a process-local mutex. Its parent kills it to simulate a crash.
func TestTransactionLockChild(t *testing.T) {
	if os.Getenv("DEPENGINE_TRANSACTION_CHILD") != "1" {
		return
	}
	guard, err := AcquireTransaction(context.Background(), os.Getenv("DEPENGINE_TRANSACTION_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.Close() }()
	if err := os.WriteFile(os.Getenv("DEPENGINE_TRANSACTION_READY"), []byte("ready"), 0o600); err != nil { // #nosec G703 -- path comes from the parent test process in a temporary directory.
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestTransactionSerializesProcessesAndRecoversAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	ready := filepath.Join(t.TempDir(), "ready")
	child := exec.Command(os.Args[0], "-test.run=^TestTransactionLockChild$") // #nosec G204,G702 -- re-executes this test binary, no external command.
	child.Env = append(os.Environ(),
		"DEPENGINE_TRANSACTION_CHILD=1",
		"DEPENGINE_TRANSACTION_PATH="+path,
		"DEPENGINE_TRANSACTION_READY="+ready,
	)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(ready)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("child never acquired lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	if guard, err := AcquireTransaction(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			_ = guard.Close()
		}
		t.Fatalf("concurrent acquisition = %v, want context deadline exceeded", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	guard, err := AcquireTransaction(context.Background(), path)
	if err != nil {
		t.Fatalf("lock remained held after process crash: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionLockAliasesAndCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "depengine.lock")
	guard, err := AcquireTransaction(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.Close() }()
	alias := filepath.Join(dir, ".", "depengine.lock")
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(dir, link); err == nil {
			alias = filepath.Join(link, "depengine.lock")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if g, err := AcquireTransaction(ctx, alias); !errors.Is(err, context.DeadlineExceeded) {
		if err == nil {
			_ = g.Close()
		}
		t.Fatalf("alias acquisition = %v, want deadline exceeded", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := AcquireTransaction(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition = %v, want context cancelled", err)
	}
}

func TestTransactionRejectsLockfileSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink privileges vary")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.lock")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.lock")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	_, err := AcquireTransaction(context.Background(), alias)
	if err == nil || !strings.Contains(err.Error(), "symbolic-link") {
		t.Fatalf("symlink acquisition = %v, want rejection", err)
	}
}

func TestTransactionSerializesReadModifyWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depengine.lock")
	first, err := AcquireTransaction(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, &Lock{Version: 1, Tools: map[string]ToolPin{"alpha/http/0": {Latest: "1"}}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		second, err := AcquireTransaction(ctx, path)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = second.Close() }()
		lk, err := Load(path)
		if err != nil {
			done <- err
			return
		}
		lk.Tools["beta/http/0"] = ToolPin{Latest: "2"}
		done <- Save(path, lk)
	}()
	// The second writer cannot read-and-write until the first finishes.
	select {
	case err := <-done:
		t.Fatalf("second transaction escaped lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	lk, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lk.Tools) != 2 {
		t.Fatalf("lost update: tools = %#v", lk.Tools)
	}
}

func TestTransactionDifferentLockfilesRemainIndependent(t *testing.T) {
	dir := t.TempDir()
	first, err := AcquireTransaction(context.Background(), filepath.Join(dir, "desktop.toml.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	second, err := AcquireTransaction(ctx, filepath.Join(dir, "server.toml.lock"))
	if err != nil {
		t.Fatalf("independent lock blocked: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
