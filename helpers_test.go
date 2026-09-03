package main

import (
	"flag"
	"io"
	"testing"
)

func TestParseFlagsInterspersed(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSchema string
		wantJSON   bool
		wantPos    []string
	}{
		{
			name:       "flags before positional",
			args:       []string{"--schema", "ex.toml", "zsh"},
			wantSchema: "ex.toml",
			wantPos:    []string{"zsh"},
		},
		{
			// regression: flag package stops at first positional, swallowing
			// trailing flags ("depengine why zsh --schema ex.toml" used default schema).
			name:       "flags after positional",
			args:       []string{"zsh", "--schema", "ex.toml"},
			wantSchema: "ex.toml",
			wantPos:    []string{"zsh"},
		},
		{
			name:       "interspersed multiple positionals",
			args:       []string{"fzf", "--json", "zsh", "--schema=ex.toml"},
			wantSchema: "ex.toml",
			wantJSON:   true,
			wantPos:    []string{"fzf", "zsh"},
		},
		{
			name:       "bool flag between positionals",
			args:       []string{"fzf", "--json", "zsh"},
			wantSchema: "schema.toml",
			wantJSON:   true,
			wantPos:    []string{"fzf", "zsh"},
		},
		{
			name:       "double-dash terminator keeps dashes positional",
			args:       []string{"--schema=ex.toml", "--", "-notflag", "zsh"},
			wantSchema: "ex.toml",
			wantPos:    []string{"-notflag", "zsh"},
		},
		{
			name:       "no positionals",
			args:       []string{"--schema", "ex.toml"},
			wantSchema: "ex.toml",
			wantPos:    nil,
		},
		{
			name:       "only positional",
			args:       []string{"zsh"},
			wantSchema: "schema.toml",
			wantPos:    []string{"zsh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			schema := fs.String("schema", "schema.toml", "")
			jsonFlag := fs.Bool("json", false, "")

			got := parseFlagsInterspersed(fs, tt.args)

			if *schema != tt.wantSchema {
				t.Errorf("schema = %q, want %q", *schema, tt.wantSchema)
			}
			if *jsonFlag != tt.wantJSON {
				t.Errorf("json = %v, want %v", *jsonFlag, tt.wantJSON)
			}
			if len(got) != len(tt.wantPos) {
				t.Fatalf("positionals = %v, want %v", got, tt.wantPos)
			}
			for i := range got {
				if got[i] != tt.wantPos[i] {
					t.Fatalf("positionals = %v, want %v", got, tt.wantPos)
				}
			}
		})
	}
}
