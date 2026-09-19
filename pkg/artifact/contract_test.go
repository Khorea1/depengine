package artifact

import (
	"errors"
	"testing"
)

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
		{name: "embedded credentials", raw: "https://secret-token@example.com/tool.tar.gz", wantErr: true},
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

func TestContractValidateArtifact(t *testing.T) {
	tests := []struct {
		name     string
		contract *Contract
		raw      string
		wantType any
	}{
		{name: "allowed raw payload", contract: &Contract{ForbiddenExtensions: PlatformInstallerExtensions}, raw: "https://example.com/tool.tar.gz"},
		{name: "supported compound archive", contract: &Contract{UnsupportedArchiveExtensions: UnsupportedArchiveExtensions}, raw: "https://example.com/tool.tar.xz"},
		{name: "forbidden installer", contract: &Contract{ForbiddenExtensions: PlatformInstallerExtensions}, raw: "https://example.com/tool.DMG?download=1", wantType: &ForbiddenExtensionError{}},
		{name: "unsupported archive", contract: &Contract{UnsupportedArchiveExtensions: UnsupportedArchiveExtensions}, raw: "tool.7z", wantType: &UnsupportedArchiveError{}},
		{name: "required extension", contract: &Contract{RequiredExtensions: []string{".apk"}}, raw: "tool.zip", wantType: &RequiredExtensionError{}},
		{name: "required case insensitive", contract: &Contract{RequiredExtensions: []string{".AppImage"}}, raw: "Tool.APPIMAGE#asset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.contract.ValidateArtifact(tt.raw)
			if tt.wantType == nil {
				if err != nil {
					t.Fatalf("ValidateArtifact() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateArtifact() error = nil, want error")
			}
			switch tt.wantType.(type) {
			case *ForbiddenExtensionError:
				var target *ForbiddenExtensionError
				if !errors.As(err, &target) {
					t.Fatalf("error type = %T, want *ForbiddenExtensionError", err)
				}
			case *UnsupportedArchiveError:
				var target *UnsupportedArchiveError
				if !errors.As(err, &target) {
					t.Fatalf("error type = %T, want *UnsupportedArchiveError", err)
				}
			case *RequiredExtensionError:
				var target *RequiredExtensionError
				if !errors.As(err, &target) {
					t.Fatalf("error type = %T, want *RequiredExtensionError", err)
				}
			}
		})
	}
}
