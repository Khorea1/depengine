package lang

import (
	"context"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func TestParseMajorVersion(t *testing.T) {
	cases := []struct {
		in    string
		want  int
		found bool
	}{
		{"1.22.19", 1, true},
		{"2.0.0", 2, true},
		{"3.1.0", 3, true},
		{"v2.1.0", 2, true},
		{"v10.0.1", 10, true},
		{"20.0.0", 20, true},
		{"berry-2.0.0", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseMajorVersion(tc.in)
			if ok != tc.found {
				t.Fatalf("parseMajorVersion(%q) found = %v, want %v", tc.in, ok, tc.found)
			}
			if ok && got != tc.want {
				t.Fatalf("parseMajorVersion(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestYarnBerryAvailableVersionGating(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   bool
	}{
		{"classic yarn 1.x", "1.22.19\n", false},
		{"berry 2.x", "2.0.0\n", true},
		{"berry 3.x", "3.1.0\n", true},
		{"v-prefixed berry", "v2.1.0\n", true},
		{"multi-digit major", "10.0.0\n", true},
		{"non-numeric prefix", "berry-2.0.0\n", false},
		{"empty version", "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &run.FakeRunner{ExitCode: 0, Stdout: tc.stdout}
			adapter := NewYarnBerryAdapter()
			got := adapter.Available(context.Background(), fr)
			if got != tc.want {
				t.Fatalf("Available with stdout %q = %v, want %v", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestPacstallCheckUsesCorrectFlag(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewPacstallAdapter()
	tool := &schema.Tool{Name: "test"}
	mc := &schema.MethodCandidate{Config: map[string]any{"pkg": "foo"}}

	adapter.Check(context.Background(), fr, tool, mc)

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	call := fr.Calls[0]
	if call.Name != "pacstall" {
		t.Fatalf("expected command 'pacstall', got %q", call.Name)
	}
	if len(call.Args) != 2 || call.Args[0] != "-Ci" || call.Args[1] != "foo" {
		t.Fatalf("expected args ['-Ci', 'foo'], got %v", call.Args)
	}
}

func TestSteamCMDCheckWithEmptyInstallDir(t *testing.T) {
	adapter := NewSteamCMDAdapter()
	tool := &schema.Tool{Name: "test"}

	t.Run("returns false when dir config is empty string", func(t *testing.T) {
		mcWithEmptyDir := &schema.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": ""},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithEmptyDir)
		if got {
			t.Fatal("Check should return false when dir is empty string")
		}
	})

	t.Run("returns false when no pkg", func(t *testing.T) {
		mcNoPkg := &schema.MethodCandidate{Config: map[string]any{}}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcNoPkg)
		if got {
			t.Fatal("Check should return false when pkg is missing")
		}
	})

	t.Run("uses explicit dir when provided", func(t *testing.T) {
		mcWithDir := &schema.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": "/tmp"},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithDir)
		if got {
			t.Fatal("Check should return false (always checks for updates)")
		}
	})
}
