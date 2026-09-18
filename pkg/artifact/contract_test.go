package artifact

import "testing"

func TestValidateURL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https", raw: "https://example.com/tool.tar.gz"},
		{name: "http", raw: "http://example.com/tool.tar.gz"},
		{name: "unsupported scheme", raw: "ftp://example.com/tool.tar.gz", wantErr: true},
		{name: "missing host", raw: "https:///tool.tar.gz", wantErr: true},
		{name: "relative", raw: "tool.tar.gz", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateURL(tc.raw, []string{"http", "https"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateURL(%q) error = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestInstallerExtension(t *testing.T) {
	for _, ext := range PlatformInstallerExtensions {
		if got := InstallerExtension("https://example.com/Tool" + ext + "?download=1#asset"); got != ext {
			t.Fatalf("InstallerExtension(%q) = %q, want %q", ext, got, ext)
		}
	}
	if got := InstallerExtension("https://example.com/tool.tar.gz"); got != "" {
		t.Fatalf("archive classified as installer: %q", got)
	}
}
