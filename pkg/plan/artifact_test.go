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
