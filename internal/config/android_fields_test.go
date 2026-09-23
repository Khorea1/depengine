package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseAndroidRejectsArchiveOnlyFields(t *testing.T) {
	tests := []struct {
		field string
		value string
	}{
		{field: "strip_components", value: "1"},
		{field: "entrypoints", value: "{ demo = \"bin/demo\" }"},
		{field: "link_dir", value: "\"~/.local/bin\""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			path := writeTempSchema(t, fmt.Sprintf(`
[tools.demo.android]
url = "https://example.test/demo.apk"
%s = %s
`, tt.field, tt.value))

			_, err := ParseProjectSchema(path, nil)
			if err == nil {
				t.Fatalf("ParseProjectSchema() accepted android.%s, which has no APK semantics", tt.field)
			}
			want := "tools.demo.android." + tt.field + ": field is not supported by method kind android"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("ParseProjectSchema() error = %v, want %q", err, want)
			}
		})
	}
}

func TestParseAndroidKeepsTransportIntegrityFields(t *testing.T) {
	path := writeTempSchema(t, `
[tools.demo.android]
url = "https://example.test/demo.apk"
checksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
sudo_required = false
`)
	if _, err := ParseProjectSchema(path, nil); err != nil {
		t.Fatalf("ParseProjectSchema() rejected supported Android transport fields: %v", err)
	}
}
