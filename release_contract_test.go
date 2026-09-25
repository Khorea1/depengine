package main

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseWorkflowContract(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)

	for _, invariant := range []struct{ description, value string }{
		{"version tag push trigger", "push:\n    tags:\n      - 'v*'"},
		{"exact Go version", "go-version: '1.27.1'"},
		{"job-scoped contents write permission", "build-and-publish:\n    permissions:\n      contents: write"},
		{"GoReleaser version v2.17.0", "version: v2.17.0"},
		{"GitHub Actions token", "GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}"},
	} {
		if !strings.Contains(workflow, invariant.value) {
			t.Errorf("release workflow is missing %s", invariant.description)
		}
	}
	for _, invariant := range []struct{ description, value string }{
		{"published-release trigger", "types: [published]"},
		{"release-event tag reference", "github.event.release.tag_name"},
		{"write-all permission", "write-all"},
	} {
		if strings.Contains(workflow, invariant.value) {
			t.Errorf("release workflow still contains %s", invariant.description)
		}
	}
	if got := strings.Count(workflow, "goreleaser/goreleaser-action"); got != 1 {
		t.Errorf("GoReleaser action invocation count = %d, want 1", got)
	}
	if got := strings.Count(workflow, "release --clean"); got != 1 {
		t.Errorf("GoReleaser release --clean count = %d, want 1", got)
	}
}

func TestGoReleaserRepositoryContract(t *testing.T) {
	data, err := os.ReadFile(".goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "release:\n  github:\n    owner: Khorea1\n    name: depengine\n") {
		t.Error("GoReleaser release repository must be Khorea1/depengine")
	}
}

func TestReleaseSigningContract(t *testing.T) {
	releaser, err := os.ReadFile(".goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(releaser)
	for _, invariant := range []struct{ description, value string }{
		{"distinct signature output", `"--output-signature=${signature}"`},
		{"distinct certificate output", `"--output-certificate=${certificate}"`},
		{"certificate filename declaration", `certificate: "${artifact}.pem"`},
	} {
		if !strings.Contains(cfg, invariant.value) {
			t.Errorf("GoReleaser signing config is missing %s", invariant.description)
		}
	}
	if strings.Contains(cfg, `"--output-certificate=${signature}"`) {
		t.Error("GoReleaser certificate and signature must not share one output")
	}

	workflow, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	// Cosign v3 removed --output-certificate/--output-signature; the split
	// .sig/.pem format requires a v2 pin.
	if !strings.Contains(string(workflow), "cosign-release: 'v2") {
		t.Error("release workflow must pin cosign to v2 for split .sig/.pem signing")
	}

	security, err := os.ReadFile("docs/security.md")
	if err != nil {
		t.Fatal(err)
	}
	docs := string(security)
	if strings.Contains(docs, "certificate-identity-regexp '.*'") {
		t.Error("verification docs must not trust any OIDC identity")
	}
	if !strings.Contains(docs, `Khorea1/depengine/\.github/workflows/release\.yml@refs/tags/`) {
		t.Error("verification docs must pin the release workflow identity")
	}
}

func goReleaserBuildBlock(config, id string) (string, bool) {
	inBuilds := false
	collecting := false
	var block strings.Builder
	for _, line := range strings.Split(config, "\n") {
		if !inBuilds {
			if line == "builds:" {
				inBuilds = true
			}
			continue
		}
		if line != "" && line[0] != ' ' {
			break
		}
		if strings.HasPrefix(line, "  - id: ") {
			if collecting {
				break
			}
			collecting = strings.TrimSpace(strings.TrimPrefix(line, "  - id: ")) == id
		}
		if collecting {
			block.WriteString(line)
			block.WriteByte('\n')
		}
	}
	return block.String(), block.Len() > 0
}

func TestGoReleaserStaticBuildContract(t *testing.T) {
	data, err := os.ReadFile(".goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	build, ok := goReleaserBuildBlock(string(data), "depengine")
	if !ok {
		t.Fatal("GoReleaser build depengine is missing")
	}
	if !strings.Contains(build, "    env:\n      - CGO_ENABLED=0\n") {
		t.Error("GoReleaser depengine build must set CGO_ENABLED=0")
	}
}
