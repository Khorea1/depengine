package containerref

import "testing"

func TestValidateRepository(t *testing.T) {
	for _, source := range []string{"redis", "library/redis", "ghcr.io/owner/tool", "registry.example:5000/team/tool"} {
		if err := ValidateRepository(source); err != nil {
			t.Errorf("ValidateRepository(%q) = %v", source, err)
		}
	}
	for _, source := range []string{"", "redis:7", "ghcr.io/owner/tool@sha256:deadbeef", "https://ghcr.io/owner/tool", "/redis", "owner//tool", "owner/tool/", "owner/tool latest"} {
		if err := ValidateRepository(source); err == nil {
			t.Errorf("ValidateRepository(%q) = nil, want error", source)
		}
	}
}

func TestValidateTag(t *testing.T) {
	for _, tag := range []string{"", "latest", "7", "v1.2.3", "release_candidate-1"} {
		if err := ValidateTag(tag); err != nil {
			t.Errorf("ValidateTag(%q) = %v", tag, err)
		}
	}
	for _, tag := range []string{"-bad", ".bad", "team/release", "v1:2", "latest@sha256:deadbeef", "two words"} {
		if err := ValidateTag(tag); err == nil {
			t.Errorf("ValidateTag(%q) = nil, want error", tag)
		}
	}
}

func TestValidateDigest(t *testing.T) {
	good := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, digest := range []string{"", good, "SHA256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"} {
		if err := ValidateDigest(digest); err != nil {
			t.Errorf("ValidateDigest(%q) = %v", digest, err)
		}
	}
	for _, digest := range []string{"sha512:abcd", "sha256:abcd", "sha256:zz23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if err := ValidateDigest(digest); err == nil {
			t.Errorf("ValidateDigest(%q) = nil, want error", digest)
		}
	}
}

func TestReference(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		source, tag, digest, want string
		wantErr                   bool
	}{
		{"redis", "", "", "redis:latest", false},
		{"redis", "7", "", "redis:7", false},
		{"ghcr.io/owner/tool", "", digest, "ghcr.io/owner/tool@" + digest, false},
		{"redis", "7", digest, "", true},
	}
	for _, tt := range tests {
		got, err := Reference(tt.source, tt.tag, tt.digest)
		if (err != nil) != tt.wantErr {
			t.Fatalf("Reference(%q,%q,%q) error=%v, wantErr=%v", tt.source, tt.tag, tt.digest, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("Reference(%q,%q,%q)=%q, want %q", tt.source, tt.tag, tt.digest, got, tt.want)
		}
	}
}

func TestNormalizePlatform(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"linux/amd64", "linux/amd64", true},
		{"LINUX/ARM64/V8", "linux/arm64/v8", true},
		{" windows/amd64 ", "windows/amd64", true},
		{"linux", "", false},
		{"linux/", "", false},
		{"linux/amd64/v8/extra", "", false},
		{"linux/amd 64", "", false},
	}
	for _, tt := range tests {
		got, err := NormalizePlatform(tt.in)
		if tt.ok && err != nil {
			t.Errorf("NormalizePlatform(%q) = error %v", tt.in, err)
			continue
		}
		if !tt.ok && err == nil {
			t.Errorf("NormalizePlatform(%q) = %q, want error", tt.in, got)
			continue
		}
		if tt.ok && got != tt.want {
			t.Errorf("NormalizePlatform(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
