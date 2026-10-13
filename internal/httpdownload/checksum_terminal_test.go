package httpdownload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
)

func TestChecksumMismatchIsTerminalIntegrityFailure(t *testing.T) {
	payload := []byte("depengine-integrity-probe")
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	err := VerifyChecksum(path, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want *ChecksumMismatchError", err)
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch classification", err)
	}
	// A mismatch is an integrity failure: the executor's terminal hub must
	// abort the tool instead of silently switching source.
	if !errors.Is(err, exec.ErrIntegrityMismatch) {
		t.Fatalf("err = %v, want exec.ErrIntegrityMismatch classification", err)
	}
	if !strings.HasPrefix(err.Error(), "checksum mismatch: expected ") {
		t.Fatalf("diagnostic message changed: %q", err.Error())
	}
	actual, hashErr := SHA256File(path)
	if hashErr != nil {
		t.Fatal(hashErr)
	}
	if err := VerifyChecksum(path, "sha256:"+actual); err != nil {
		t.Fatalf("matching checksum must verify: %v", err)
	}
}
