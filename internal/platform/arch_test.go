package platform

import "testing"

func TestNormalizeRuntimeArch(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "amd64", want: "x86_64"},
		{in: "arm64", want: "aarch64"},
		{in: "386", want: "i386"},
		{in: "riscv64", want: "riscv64"},
		{in: "", want: "unknown"},
	}
	for _, tt := range tests {
		if got := normalizeRuntimeArch(tt.in); got != tt.want {
			t.Errorf("normalizeRuntimeArch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
