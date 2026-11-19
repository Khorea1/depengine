package httpdownload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
)

func TestHTTPProspectiveEffectsIdentifiesAbsentAndPresentRawTarget(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "bin")
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	tool := &config.Tool{Name: "sample"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"extract_to": installRoot, "binary": "sample", "url": "https://stale.example/archive.tar.gz",
	}}
	resolved := plan.New("sample", "http", true)
	resolved.Artifacts = []plan.Artifact{{URL: "https://example.invalid/download?key=redacted"}}
	resolved.Prerequisites = []plan.Prerequisite{{Name: "curl"}}
	before := resolved.Clone()
	want := plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: filepath.Join(installRoot, "sample")}

	check := func(expected plan.ProspectiveTarget) {
		t.Helper()
		envelope, err := NewHTTPAdapter().DescribeProspectiveEffects(tool, mc, &resolved, plan.TransitionInstall)
		if err != nil {
			t.Fatal(err)
		}
		if envelope.Completeness != plan.EffectUnknown || len(envelope.Steps) != 3 {
			t.Fatalf("unexpected envelope: %+v", envelope)
		}
		for _, step := range envelope.Steps {
			if step.Completeness != plan.EffectUnknown {
				t.Fatalf("step unexpectedly classified as exhaustive: %+v", step)
			}
			if step.Phase == "primary_install" {
				if !reflect.DeepEqual(step.Targets, []plan.ProspectiveTarget{expected}) {
					t.Fatalf("primary targets = %+v; want %+v", step.Targets, expected)
				}
			} else if len(step.Targets) != 0 {
				t.Fatalf("other phase claimed HTTP target: %+v", step)
			}
		}
		if !reflect.DeepEqual(resolved, before) {
			t.Fatal("effect probe mutated the resolved plan")
		}
		encoded, err := json.Marshal(envelope)
		if err != nil || len(encoded) == 0 {
			t.Fatalf("effect envelope JSON: %s, %v", encoded, err)
		}
	}

	check(want)                                                                               // Target does not yet exist: still a prospective file slot.
	if err := os.WriteFile(want.Identity, []byte("existing executable"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	check(want)

	if runtime.GOOS != "windows" {
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(installRoot, alias); err != nil {
			t.Fatal(err)
		}
		mc.Config["extract_to"] = alias
		check(want) // Directory aliases have the same tentative slot.
	}
}

func TestHTTPProspectiveEffectsDoNotInventTargets(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "bin")
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	tool := &config.Tool{Name: "sample"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"binary": "sample", "extract_to": installRoot,
	}}
	resolved := plan.New("sample", "http", true)
	resolved.Artifacts = []plan.Artifact{{URL: "https://example.invalid/sample"}}

	checkNone := func(label string, adapter *HTTPAdapter, transition plan.TransitionKind) {
		t.Helper()
		result, err := adapter.DescribeProspectiveEffects(tool, mc, &resolved, transition)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if result.Completeness != plan.EffectUnknown {
			t.Fatalf("%s: claimed complete effects: %+v", label, result)
		}
		for _, step := range result.Steps {
			if step.Completeness != plan.EffectUnknown || len(step.Targets) > 0 {
				t.Fatalf("%s: invented resource target: %+v", label, step)
			}
		}
	}

	checkNone("remove is not install", NewHTTPAdapter(), plan.TransitionRemove)

	original := resolved.Artifacts[0]
	for _, url := range []string{
		"https://example.invalid/sample.tar.gz",
		"https://example.invalid/sample.zip",
		"https://example.invalid/sample.bz2",
		"https://example.invalid/setup.msi",
		"https://example.invalid/setup.exe",
		"file:///tmp/sample",
	} {
		resolved.Artifacts[0].URL = url
		checkNone(url, NewHTTPAdapter(), plan.TransitionInstall)
	}
	resolved.Artifacts[0] = original
	checkNone("installer transport", NewInstallerHTTPAdapter(), plan.TransitionInstall)

	oldRoot := mc.Config["extract_to"]
	delete(mc.Config, "extract_to")
	checkNone("PATH-only", NewHTTPAdapter(), plan.TransitionInstall)
	mc.Config["extract_to"] = filepath.Join(root, "missing-parent")
	checkNone("missing parent", NewHTTPAdapter(), plan.TransitionInstall)
	mc.Config["extract_to"] = oldRoot

	delete(mc.Config, "binary")
	checkNone("implicit binary basename", NewHTTPAdapter(), plan.TransitionInstall)
	mc.Config["binary"] = "../escape"
	checkNone("invalid binary", NewHTTPAdapter(), plan.TransitionInstall)
	mc.Config["binary"] = "sample"

	mc.Kind = "github"
	checkNone("different adapter", NewHTTPAdapter(), plan.TransitionInstall)
	mc.Kind = "http"
	resolved.Candidate.Method = "github"
	checkNone("mismatched resolved candidate", NewHTTPAdapter(), plan.TransitionInstall)
	resolved.Candidate.Method = "http"

	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "other"), filepath.Join(installRoot, "sample")); err != nil {
			t.Fatal(err)
		}
		checkNone("symlink file slot", NewHTTPAdapter(), plan.TransitionInstall)
	}
	if err := os.Mkdir(filepath.Join(installRoot, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}
	mc.Config["binary"] = "occupied"
	checkNone("directory occupies slot", NewHTTPAdapter(), plan.TransitionInstall)
}

func TestHTTPProspectiveEffectsRejectsMalformedPlan(t *testing.T) {
	tool := &config.Tool{Name: "sample"}
	mc := &config.MethodCandidate{Kind: "http", Config: map[string]any{}}
	if _, err := NewHTTPAdapter().DescribeProspectiveEffects(tool, mc, nil, plan.TransitionInstall); err == nil {
		t.Fatal("nil plan unexpectedly accepted")
	}
}
