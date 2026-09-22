package engine

import "testing"

func TestCompareVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"22.04", "24.04", -1},
		{"24.04", "22.04", 1},
		{"24.04", "24.4", 0},
		{"14.6.1", "14.6", 1},
		{"10.0.26100", "10.0.22631", 1},
		{"9", "09.0", 0},
		{"1.0-rc1", "1.0-rc2", -1},
		{"1.0-A", "1.0-a", 0},
	}
	for _, tt := range tests {
		if got := CompareVersion(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersion(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
