package term

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWidthNonTerminalReturnsZero(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "not-a-terminal"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()

	if got := Width(f); got != 0 {
		t.Errorf("Width(non-terminal) = %d, want 0", got)
	}
}

func TestWidthNilFileReturnsZero(t *testing.T) {
	var missing *os.File
	if got := Width(missing); got != 0 {
		t.Errorf("Width(nil) = %d, want 0", got)
	}
}

func TestOutputWidthFallsBackToDefault(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()

	// Pipes carry no terminal geometry, so both streams fall through.
	if got := outputWidth(reader, writer); got != DefaultWidth {
		t.Errorf("outputWidth(pipes) = %d, want %d", got, DefaultWidth)
	}
}
