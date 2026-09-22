package httpdownload

import (
	"context"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
)

func TestToMSYSPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{`C:\Users\runner\Temp\x`, "/c/Users/runner/Temp/x"},
		{`D:\a\b`, "/d/a/b"},
		{`C:/already/forward`, "/c/already/forward"},
		{`\\server\share\x`, "//server/share/x"},
		{`/posix/path`, "/posix/path"},
		{`relative\path`, "relative/path"},
		{``, ""},
	}
	for _, tt := range tests {
		if got := toMSYSPath(tt.in); got != tt.want {
			t.Errorf("toMSYSPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGPGPathFollowsProbe(t *testing.T) {
	if got := gpgPath(false, `C:\x`); got != `C:\x` {
		t.Fatalf("gpgPath(false, ...) = %q, want passthrough", got)
	}
	if got := gpgPath(true, `C:\x`); got != `/c/x` {
		t.Fatalf("gpgPath(true, ...) = %q, want /c/x", got)
	}
}

func TestIsMSYSGPGFalseOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("probe is live on Windows; nothing to short-circuit")
	}
	fr := &run.FakeRunner{}
	if isMSYSGPG(context.Background(), fr) {
		t.Fatal("isMSYSGPG should be false off Windows")
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("off-Windows probe made %d runner calls, want 0", len(fr.Calls))
	}
}
