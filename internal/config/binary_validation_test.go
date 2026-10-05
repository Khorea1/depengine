package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseHTTPBinaryMustBeBasename(t *testing.T) {
	for _, name := range []string{"/tmp/escape", "../outside", "bin/tool", `C:\outside`, `dir\tool`} {
		t.Run(name, func(t *testing.T) {
			path := writeTempSchema(t, "schema_version = 1\n[tools]\ntool = { http = { url = \"https://example.com/tool\", binary = "+strconv.Quote(name)+" } }\n")
			_, err := ParseProjectSchema(path, nil)
			if err == nil || !strings.Contains(err.Error(), "binary") {
				t.Fatalf("ParseProjectSchema accepted unsafe binary %q: %v", name, err)
			}
		})
	}
}

func TestParseHTTPBinaryAcceptsBasename(t *testing.T) {
	path := writeTempSchema(t, "schema_version = 1\n[tools]\ntool = { http = { url = \"https://example.com/tool\", binary = \"wave-tool\" } }\n")
	if _, err := ParseProjectSchema(path, nil); err != nil {
		t.Fatalf("ParseProjectSchema rejected basename: %v", err)
	}
}
