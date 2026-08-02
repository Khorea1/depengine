package state

import "testing"

func TestVersionOutdated(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		pinned    string
		want      bool
	}{
		{"same version", "v1.2.3", "v1.2.3", false},
		{"v prefix normalized", "1.2.3", "v1.2.3", false},
		{"uppercase V normalized", "V1.2.3", "v1.2.3", false},
		{"whitespace trimmed", "  v1.2.3  ", "v1.2.3", false},
		{"missing trailing segments equal", "v1.2", "v1.2.0", false},
		{"numeric not lexicographic", "v1.10.0", "v1.9.0", true},
		{"different minor", "v1.2.3", "v1.3.0", true},
		{"different patch", "v4.30.0", "v4.30.1", true},
		{"pre-release suffix differs", "v1.2.3", "v1.2.3-rc1", true},
		{"empty installed", "", "v1.2.3", false},
		{"empty pinned", "v1.2.3", "", false},
		{"both empty", "", "", false},
		{"date-like tags equal", "2024-01-01", "2024-01-01", false},
		{"date-like tags differ", "2024-01-01", "2024-02-01", true},
	}
	for _, c := range cases {
		got := VersionOutdated(c.installed, c.pinned)
		if got != c.want {
			t.Errorf("VersionOutdated(%q, %q) = %v, want %v", c.installed, c.pinned, got, c.want)
		}
	}
}
