package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBackedSourceRevisionSchemaContract(t *testing.T) {
	const validRevision = "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name    string
		source  string
		wantErr string
		wantRev string
	}{
		{name: "valid Brew revision retained", source: `kind = "brew-tap", name = "corp/tools", url = "https://example.test/tools.git", revision = "` + validRevision + `"`, wantRev: validRevision},
		{name: "revision requires explicit URL", source: `kind = "brew-tap", name = "corp/tools", revision = "` + validRevision + `"`, wantErr: "required when revision is set"},
		{name: "valid Scoop revision retained", source: `kind = "scoop-bucket", name = "corp-tools", url = "https://example.test/tools.git", revision = "` + validRevision + `"`, wantRev: validRevision},
		{name: "non-Git-backed source rejected", source: `kind = "apt-ppa", name = "corp/tools", revision = "` + validRevision + `"`, wantErr: "supported only for brew-tap and scoop-bucket sources"},
		{name: "short revision rejected", source: `kind = "brew-tap", name = "corp/tools", url = "https://example.test/tools.git", revision = "abcdef"`, wantErr: "full lowercase 40- or 64-character hexadecimal commit ID"},
		{name: "uppercase revision rejected", source: `kind = "brew-tap", name = "corp/tools", url = "https://example.test/tools.git", revision = "0123456789ABCDEF0123456789ABCDEF01234567"`, wantErr: "full lowercase 40- or 64-character hexadecimal commit ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema.toml")
			data := "schema_version = 1\n[tools.demo.native]\npkg = \"demo\"\nsources = [{ " + tt.source + " }]\n"
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			schema, err := ParseProjectSchema(path, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseProjectSchema() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := schema.Tools["demo"].Methods[0].Sources[0].Revision
			if got != tt.wantRev {
				t.Fatalf("revision = %q, want %q", got, tt.wantRev)
			}
		})
	}
}
