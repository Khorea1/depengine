package httpdownload

import "testing"

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

func TestGPGPathPassesThroughOffWindows(t *testing.T) {
	if msysGPG() {
		t.Skip("MSYS gpg present: conversion active, passthrough does not apply")
	}
	in := `C:\Users\runner\Temp\x`
	if got := gpgPath(in); got != in {
		t.Fatalf("gpgPath(%q) = %q, want passthrough", in, got)
	}
}
