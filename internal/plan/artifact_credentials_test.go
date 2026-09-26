package plan

import "testing"

func TestArtifactRejectsCredentialBearingURLs(t *testing.T) {
	for _, artifact := range []Artifact{
		{URL: "https://user:secret@example.test/demo.tar.gz"}, // #nosec G101 -- synthetic credential is required to exercise secret/credential handling.
		{URL: "https://example.test/demo.tar.gz", SignatureURL: "https://example.test/demo.sig?token=secret"},
	} {
		if err := artifact.Validate(); err == nil {
			t.Fatalf("Validate() accepted %+v", artifact)
		}
	}
}
