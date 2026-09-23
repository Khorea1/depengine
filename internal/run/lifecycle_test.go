package run

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

// A grandchild that inherits the child's stdout pipe keeps it open after
// the direct child exits, so a plain cmd.Wait blocks until the grandchild
// leaves. Cancelling the context must still return promptly: Cancel
// SIGTERMs the whole process group and WaitDelay bounds the wait even if
// a grandchild ignores the signal.
func TestRunReturnsDespiteGrandchildHoldingPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process-group semantics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	// sleep inherits sh's stdout; wait keeps sh alive until sleep ends.
	// Killing only sh would orphan sleep and block Wait for 60s.
	res := OSExecRunner{}.Run(ctx, "sh", "-c", "sleep 60 & wait")
	elapsed := time.Since(start)

	if !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Fatalf("Err = %v, want DeadlineExceeded", res.Err)
	}
	// 60s of grandchild lifetime must not leak into the caller: the
	// 2s timeout plus the 5s WaitDelay bound this far below it.
	if elapsed > 30*time.Second {
		t.Fatalf("Run blocked %v with a grandchild holding the pipe", elapsed)
	}
}

// A grandchild that ignores SIGTERM and holds the pipe must not block
// Run past WaitDelay, even though Cancel cannot stop it: the direct
// child exits on its own, so no Cancel fires and Wait gives up on the
// orphaned pipe after the grace period.
func TestRunUnblocksWhenGrandchildIgnoresSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal semantics")
	}
	start := time.Now()
	// trap "" TERM is inherited across fork+exec: the background sleep
	// ignores the group signal and holds sh's stdout for 60s, while the
	// exec'd sleep exits in ~1s leaving only the grandchild behind.
	res := OSExecRunner{}.Run(context.Background(), "sh", "-c", `trap "" TERM; sleep 60 & exec sleep 1`)
	elapsed := time.Since(start)

	if !errors.Is(res.Err, exec.ErrWaitDelay) {
		t.Fatalf("Err = %v, want ErrWaitDelay", res.Err)
	}
	// ~1s of child lifetime plus the 5s WaitDelay must stay far below
	// the 60s the grandchild would otherwise impose.
	if elapsed > 30*time.Second {
		t.Fatalf("Run blocked %v despite WaitDelay", elapsed)
	}
}

// Output far larger than the cap must not grow memory unbounded: each
// stream retains only its tail.
func TestCappedBufferRetainsTail(t *testing.T) {
	b := newCappedBuffer(8)
	for _, chunk := range []string{"abcd", "efgh", "ijkl", "mnop"} {
		if _, err := b.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got := string(b.Bytes()); got != "ijklmnop" {
		t.Fatalf("Bytes = %q, want %q", got, "ijklmnop")
	}
	// A single write larger than the cap keeps only its own tail.
	if _, err := b.Write([]byte("0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := string(b.Bytes()); got != "89abcdef" {
		t.Fatalf("Bytes = %q, want %q", got, "89abcdef")
	}
}

func TestRunCapturedOutputIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX producer pipeline")
	}
	// awk is POSIX and produces exactly 3,000,000 bytes here. Avoid
	// head -c: OpenBSD head(1) intentionally supports line counts only.
	res := OSExecRunner{}.Run(context.Background(), "awk", "BEGIN { for (i = 0; i < 1500000; i++) print \"y\" }")
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("producer failed: %+v", res)
	}
	if len(res.Stdout) > maxCapturedOutput {
		t.Fatalf("captured stdout = %d bytes, want <= %d", len(res.Stdout), maxCapturedOutput)
	}
	if len(res.Stderr) > maxCapturedOutput {
		t.Fatalf("captured stderr = %d bytes, want <= %d", len(res.Stderr), maxCapturedOutput)
	}
	if !bytes.HasSuffix(res.Stdout, []byte("y\n")) {
		t.Fatal("captured stdout is not the producer's tail")
	}
}

// lockedBuffer is a goroutine-safe sink for Stream, which receives stdout
// and stderr concurrently.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func TestStreamReceivesLiveOutput(t *testing.T) {
	name, args := "sh", []string{"-c", "echo live-marker"}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/d", "/c", "echo live-marker"}
	}
	var live lockedBuffer
	res := OSExecRunner{Stream: &live}.Run(context.Background(), name, args...)
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("run failed: %+v", res)
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if !bytes.Contains(live.b.Bytes(), []byte("live-marker")) {
		t.Fatalf("stream got %q, want live-marker", live.b.String())
	}
	// Capture into Result is unaffected by teeing.
	if !bytes.Contains(res.Stdout, []byte("live-marker")) {
		t.Fatalf("captured stdout %q lost the marker", res.Stdout)
	}
}
