package app

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

func TestLockPinForToolStateUsesPersistedLabel(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{primary, mirror}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	pin, ok := lockPinForToolState(lk, "demo", tool, state.ToolState{Method: "mirror", MethodKind: "http"})
	if !ok || pin.Latest != "v2" {
		t.Fatalf("lockPinForToolState = %+v, %v; want v2, true", pin, ok)
	}
}

func TestValidateInstallNPMLockIdentityRejectsDriftBeforeResolution(t *testing.T) {
	selector := fmt.Sprintf("%x", sha256.Sum256([]byte("old-package\x00https://registry.example.test")))
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: selector, PackageVersion: "1.2.3"},
	}}
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{
		"pkg": "new-package", "registry": "https://registry.example.test",
	}}
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"tool": {Name: "tool", Methods: []*config.MethodCandidate{method}},
	}}
	if err := validateInstallNPMLockIdentity(schema, lk); err == nil || !strings.Contains(err.Error(), "npm package, registry, or version request changed") {
		t.Fatalf("validateInstallNPMLockIdentity() error = %v, want stale identity rejection", err)
	}

	method.Config["pkg"] = "old-package"
	if err := validateInstallNPMLockIdentity(schema, lk); err != nil {
		t.Fatalf("matching identity rejected: %v", err)
	}

	method.Config["version"] = "1.2.3"
	if err := validateInstallNPMLockIdentity(schema, lk); err == nil {
		t.Fatal("explicit version request accepted against stale unversioned npm pin")
	}
}

func TestMergeInstallLockRejectsNPMSelectorDrift(t *testing.T) {
	old := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: strings.Repeat("a", 64), PackageVersion: "1.2.3"},
	}}
	fresh := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: strings.Repeat("b", 64), PackageVersion: "2.0.0"},
	}}
	merged, err := mergeInstallLock(old, fresh)
	if err == nil || !strings.Contains(err.Error(), "npm package or registry changed") {
		t.Fatalf("mergeInstallLock() error = %v, want npm selector drift rejection", err)
	}
	if merged != nil {
		t.Fatalf("mergeInstallLock() returned stale merge after drift: %+v", merged.Tools["tool/npm/0"])
	}
}

func TestMergeInstallLockKeepsMatchingNPMPin(t *testing.T) {
	selector := strings.Repeat("a", 64)
	old := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: selector, PackageVersion: "1.2.3"},
	}}
	fresh := &lock.Lock{Version: 1, Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: selector, PackageVersion: "1.2.3"},
	}}
	merged, err := mergeInstallLock(old, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if pin := merged.Tools["tool/npm/0"]; pin.PackageSelector != selector || pin.PackageVersion != "1.2.3" {
		t.Fatalf("matching npm pin changed during merge: %+v", pin)
	}
}

func TestNPMPackageVersionPinDrivesUpgradeDiscovery(t *testing.T) {
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "tool"}}
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{method}}
	schema := &config.Schema{Tools: map[string]*config.Tool{"tool": tool}}
	selector := fmt.Sprintf("%x", sha256.Sum256([]byte("tool\x00")))
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"tool/npm/0": {PackageSelector: selector, PackageVersion: "1.3.0"},
	}}
	st := &state.State{Tools: map[string]state.ToolState{
		"tool": {Method: "npm", MethodKind: "npm", Version: "1.2.3"},
	}}
	outdated, failures := collectOutdatedTools(st, schema, lk, "", []string{"npm"}, "")
	if len(failures) != 0 || len(outdated) != 1 || outdated[0].pinnedVer != "1.3.0" {
		t.Fatalf("outdated = %+v, failures = %+v", outdated, failures)
	}
	method.Config["pkg"] = "other"
	outdated, failures = collectOutdatedTools(st, schema, lk, "", []string{"npm"}, "")
	if len(outdated) != 0 || len(failures) != 0 {
		t.Fatalf("stale pin drove upgrade: outdated = %+v, failures = %+v", outdated, failures)
	}
}

func TestLockPinForToolStateFailsClosedForAmbiguousLegacyState(t *testing.T) {
	tool := &config.Tool{Methods: []*config.MethodCandidate{
		{Kind: "http", Label: "primary"},
		{Kind: "http", Label: "mirror"},
	}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	if pin, ok := lockPinForToolState(lk, "demo", tool, state.ToolState{Method: "http", MethodKind: "http"}); ok {
		t.Fatalf("lockPinForToolState accepted ambiguous legacy state: %+v", pin)
	}
}

func TestFindStateMethodCandidateAmbiguityRemediationUsesOnlyRealLabels(t *testing.T) {
	t.Run("labeled", func(t *testing.T) {
		tool := &config.Tool{Methods: []*config.MethodCandidate{
			{Kind: "http", Label: "primary"},
			{Kind: "http", Label: "mirror"},
		}}
		_, err := findStateMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"})
		if err == nil || !strings.Contains(err.Error(), `method = "primary"`) {
			t.Fatalf("error = %v, want labeled remediation", err)
		}
	})

	t.Run("unlabeled", func(t *testing.T) {
		tool := &config.Tool{Methods: []*config.MethodCandidate{
			{Kind: "http"},
			{Kind: "http"},
		}}
		_, err := findStateMethodCandidate(tool, state.ToolState{Method: "http", MethodKind: "http"})
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if strings.Contains(err.Error(), `method = "http"`) {
			t.Fatalf("error suggests ineffective kind-only selector: %v", err)
		}
		if !strings.Contains(err.Error(), "add distinct labels") {
			t.Fatalf("error = %v, want actionable label remediation", err)
		}
	})
}
