package ecosystem

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestLockedNPMVersionIsObservedByExecutor(t *testing.T) {
	method := &config.MethodCandidate{Kind: "npm", LockedVersion: "1.2.3", Config: map[string]any{"pkg": "tool"}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	runner := &run.FakeRunner{LookPaths: map[string]bool{"npm": true}, Stdout: `{"dependencies":{"tool":{"version":"1.2.3"}}}`}
	ex := exec.New()
	exec.WithAdapters(NewBaseAdapter(Configs["npm"]))(ex)
	exec.WithRunner(runner)(ex)
	_, verification, err := ex.ResolveAndVerifyCandidate(context.Background(), tool, method)
	if err != nil || verification.State != plan.StateSatisfied {
		t.Fatalf("pinned npm verification = %+v, err = %v", verification, err)
	}
	runner.Stdout = `{"dependencies":{"tool":{"version":"1.2.4"}}}`
	_, verification, err = ex.ResolveAndVerifyCandidate(context.Background(), tool, method)
	if err != nil || verification.State != plan.StateDrifted {
		t.Fatalf("drifted npm verification = %+v, err = %v", verification, err)
	}
}

func TestResolveLatestNPMVersionRejectsUnresolvedOutput(t *testing.T) {
	for _, output := range []string{`null`, `"latest"`, `"^1.2.3"`, `{"latest":"1.2.3"}`, `"1.2.3 --prefix=/tmp"`} {
		t.Run(output, func(t *testing.T) {
			_, err := ResolveLatestNPMVersion(context.Background(), &run.FakeRunner{Stdout: output}, "pkg", "")
			if err == nil || !strings.Contains(err.Error(), "did not resolve to a concrete version") {
				t.Fatalf("output %s: error = %v", output, err)
			}
		})
	}
}

func TestValidNPMVersionRequiresConcreteSemver(t *testing.T) {
	for _, version := range []string{"0.0.0", "1.2.3", "1.2.3-alpha.1", "1.2.3+build.01", "1.2.3-alpha.1+build.7"} {
		if !ValidNPMVersion(version) {
			t.Errorf("rejected valid version %q", version)
		}
	}
	for _, version := range []string{"", "latest", "^1.2.3", "01.2.3", "1.02.3", "1.2.03", "1.2.3-", "1.2.3-..", "1.2.3-alpha..1", "1.2.3-01", "1.2.3+", "1.2.3+build..1"} {
		if ValidNPMVersion(version) {
			t.Errorf("accepted invalid version %q", version)
		}
	}
}

func TestIsNPMRegistryPackageExcludesVersionedSpecs(t *testing.T) {
	for _, name := range []string{"tool", "@scope/tool"} {
		if !IsNPMRegistryPackage(name) {
			t.Errorf("rejected plain package %q", name)
		}
	}
	for _, spec := range []string{"tool@next", "@scope/tool@1.2.3", "file:./tool", "https://example.test/tool.tgz"} {
		if IsNPMRegistryPackage(spec) {
			t.Errorf("accepted package spec %q as plain package", spec)
		}
	}
}
