package plan_test

import (
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestNormalizeProjectPath(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "vendor/tool", want: "vendor/tool"},
		{in: "vendor/./tool", want: "vendor/tool"},
		{in: "vendor/tool/", want: "vendor/tool"},
		{in: "../tool", wantErr: true},
		{in: "/tmp/tool", wantErr: true},
		{in: "C:/vendor/tool", wantErr: true},
		{in: "c:vendor/tool", wantErr: true},
		{in: `vendor\\tool`, wantErr: true},
		{in: " tool", wantErr: true},
		// Cleaning must not hand back a value that fails these very rules:
		// stripping the trailing slash exposes trailing whitespace, and
		// removing a dot segment can expose a Windows volume prefix.
		{in: "0 /", wantErr: true},
		{in: "a/../ b", wantErr: true},
		{in: "a/../C:x", wantErr: true},
	} {
		got, err := plan.NormalizeProjectPath(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("NormalizeProjectPath(%q) error=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
		if !tc.wantErr && got != tc.want {
			t.Fatalf("NormalizeProjectPath(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestArtifactValidateRequiresExclusivePortableSource(t *testing.T) {
	cases := []plan.Artifact{
		{},
		{URL: "https://example.test/tool", LocalPath: "vendor/tool"},
		{LocalPath: "/tmp/tool"},
		{LocalPath: "vendor/../tool"},
		{Kind: "mystery", LocalPath: "vendor/tool"},
	}
	for i, artifact := range cases {
		if err := artifact.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %+v", i, artifact)
		}
	}
	if err := (plan.Artifact{Kind: plan.ArtifactRaw, LocalPath: "vendor/tool", Checksum: "sha256:abc"}).Validate(); err != nil {
		t.Fatalf("portable local artifact rejected: %v", err)
	}
}

func TestArtifactRejectsMalformedResolvedURLsAndChecksum(t *testing.T) {
	tests := []plan.Artifact{
		{URL: "tool.tar.gz"},
		{URL: "https:///tool.tar.gz"},
		{URL: "https://example.test/tool.tar.gz", SignatureURL: "tool.sig"},
		{URL: "https://example.test/tool.tar.gz", Checksum: " sha256:abc"},
		{URL: "https://example.test/tool.tar.gz", Checksum: "sha256:abc\x00bad"},
	}
	for i, artifact := range tests {
		if err := artifact.Validate(); err == nil {
			t.Fatalf("case %d: Validate() accepted malformed resolved artifact %#v", i, artifact)
		}
	}
}

func TestArtifactAllowsAbsoluteNonNetworkURL(t *testing.T) {
	artifact := plan.Artifact{URL: "file:///opt/vendor/tool.tar.gz"}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("Validate() rejected absolute file URL: %v", err)
	}
}

func TestArtifactValidatesRemoteIntegrityMetadata(t *testing.T) {
	valid := plan.Artifact{
		URL:                "https://example.test/tool.tar.gz",
		ChecksumURL:        "https://example.test/tool.tar.gz.sha256",
		ChecksumFileFormat: "sha256sum",
		SignatureURL:       "https://example.test/tool.tar.gz.sig",
		SigningKey:         "release-key-2026",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() rejected remote integrity metadata: %v", err)
	}

	for name, artifact := range map[string]plan.Artifact{
		"checksum URL": {
			URL:         "https://example.test/tool.tar.gz",
			ChecksumURL: "https://user:secret@example.test/tool.sha256",
		},
		"checksum format": {
			URL:                "https://example.test/tool.tar.gz",
			ChecksumFileFormat: "unknown",
		},
		"signing key whitespace": {
			URL:        "https://example.test/tool.tar.gz",
			SigningKey: " release-key",
		},
		"signing key URL credentials": {
			URL:        "https://example.test/tool.tar.gz",
			SigningKey: "https://user:secret@example.test/key.asc",
		},
		"local checksum URL": {
			LocalPath:   "vendor/tool.tar.gz",
			ChecksumURL: "https://example.test/tool.sha256",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := artifact.Validate(); err == nil {
				t.Fatalf("Validate() accepted invalid artifact: %+v", artifact)
			}
		})
	}
}
