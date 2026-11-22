package config

import (
	"strings"
	"testing"
)

func TestOrderSelectorsRejectHostPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "defaults method_order",
			body: `
[defaults]
method_order = ["{distro_family}", "github"]

[tools]
foo = { github = { repo = "o/foo", asset = "foo.tar.gz", checksum = "sha256:auto", scope = "user", entrypoints = { foo = "foo" } } }
`,
			want: "defaults.method_order",
		},
		{
			name: "defaults method_prefer",
			body: `
[defaults]
method_prefer = ["{arch}"]

[tools]
foo = { github = { repo = "o/foo", asset = "foo.tar.gz", checksum = "sha256:auto", scope = "user", entrypoints = { foo = "foo" } } }
`,
			want: "defaults.method_prefer",
		},
		{
			name: "tool method_prefer",
			body: `
[tools]
foo = { method_prefer = ["{distro_family}"], cargo = true }
`,
			want: "tools.foo.method_prefer",
		},
		{
			name: "tool method_only",
			body: `
[tools]
foo = { method_only = ["{os}"], cargo = true }
`,
			want: "tools.foo.method_only",
		},
		{
			name: "unknown placeholder",
			body: `
[tools]
foo = { method_prefer = ["{unknown}"], cargo = true }
`,
			want: "tools.foo.method_prefer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSchema(t, tt.body)
			_, err := ParseProjectSchema(path, fixedMap())
			if err == nil {
				t.Fatal("expected placeholder selector to be rejected")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v; want path %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "host-independent") {
				t.Fatalf("error = %v; want host-independence diagnostic", err)
			}
		})
	}
}

func TestOrderSelectorsAcceptLiteralSelectors(t *testing.T) {
	path := writeSchema(t, `
[defaults]
method_prefer = ["cargo", "github"]

[tools]
foo = { method_prefer = ["native"], cargo = true, github = { repo = "o/foo", asset = "foo.tar.gz", checksum = "sha256:auto", scope = "user", entrypoints = { foo = "foo" } } }
`)
	if _, err := ParseProjectSchema(path, fixedMap()); err != nil {
		t.Fatalf("literal selectors should parse: %v", err)
	}
}
