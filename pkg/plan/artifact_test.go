package plan_test

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestNormalizeProjectPath(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "vendor/tool", want: "vendor/tool"},
		{in: "vendor/./tool", want: "vendor/tool"},
		{in: "../tool", wantErr: true},
		{in: "/tmp/tool", wantErr: true},
		{in: "C:/vendor/tool", wantErr: true},
		{in: "c:vendor/tool", wantErr: true},
		{in: `vendor\\tool`, wantErr: true},
		{in: " tool", wantErr: true},
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
