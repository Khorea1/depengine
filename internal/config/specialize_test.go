package config

// Tests for SpecializeDistribution (proposal E, 8.3): pure per-host
// selection materializing one conventional candidate. No network.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/platform"
)

func specializeFixture(t *testing.T) *MethodCandidate {
	t.Helper()
	s, err := ParseProjectSchema(writeSchema(t, `
[tools.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[tools.br.github.distributions]]
when = { os = ["darwin"] }

[[tools.br.github.distributions]]
when = { os = ["linux"], libc = ["glibc"] }

[[tools.br.github.distributions]]
when = { os = ["linux"], libc = ["musl"] }
asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz"

[[tools.br.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "br-{version}-{os_any}_{arch_any}.zip"
entrypoints = { br = "br.exe" }
`), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	return s.Tools["br"].Methods[0]
}

func linuxGlibc() *platform.Facts {
	return &platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"}
}

func TestSpecializePassthrough(t *testing.T) {
	plain, err := ParseProjectSchema(writeSchema(t, "[tools.yq.github]\nrepo = \"mikefarah/yq\"\nasset = \"yq_{os_any}_{arch_any}\"\n"), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	mc := plain.Tools["yq"].Methods[0]
	got, err := SpecializeDistribution(mc, linuxGlibc())
	if err != nil {
		t.Fatal(err)
	}
	if got != mc {
		t.Fatal("method without distributions must pass through unchanged (same pointer)")
	}
	if _, err := SpecializeDistribution(nil, linuxGlibc()); err == nil {
		t.Fatal("nil method must error")
	}
}

func TestSpecializeSelectsOne(t *testing.T) {
	mc := specializeFixture(t)
	before, _ := json.Marshal(mc.Config)
	got, err := SpecializeDistribution(mc, linuxGlibc())
	if err != nil {
		t.Fatal(err)
	}
	if got == mc {
		t.Fatal("specialized candidate must be a copy")
	}
	if got.Distributions != nil {
		t.Fatal("specialized copy must not carry Distributions (conventional candidate)")
	}
	// Re-specialization is a no-op returning an equal candidate.
	again, err := SpecializeDistribution(got, linuxGlibc())
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(got.Config)
	jb, _ := json.Marshal(again.Config)
	if string(ja) != string(jb) {
		t.Fatal("re-specialization must be stable")
	}
	if got.Config["asset"] != "br-{version}-{os_any}_{arch_any}.tar.gz" {
		t.Fatalf("linux glibc inherits base asset, got %v", got.Config["asset"])
	}
	musl, err := SpecializeDistribution(mc, &platform.Facts{OS: "linux", TargetArch: "x86_64", Libc: "musl 1.2.4", TargetFamily: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if musl.Config["asset"] != "br-{version}-{os_any}_musl_{arch_any}.tar.gz" {
		t.Fatalf("musl asset=%v", musl.Config["asset"])
	}
	win, err := SpecializeDistribution(mc, &platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if win.Config["asset"] != "br-{version}-{os_any}_{arch_any}.zip" {
		t.Fatalf("win asset=%v", win.Config["asset"])
	}
	entrypoints, ok := win.Config["entrypoints"].(map[string]any)
	if !ok || entrypoints["br"] != "br.exe" || len(entrypoints) != 1 {
		t.Fatalf("win entrypoints must replace whole map, got %v", win.Config["entrypoints"])
	}
	if win.Config["checksum"] != "sha256:auto" || win.Config["scope"] != "user" || win.Config["repo"] != "Dicklesworthstone/beads_rust" {
		t.Fatalf("uninherited fields changed: %v", win.Config)
	}
	after, _ := json.Marshal(mc.Config)
	if string(before) != string(after) {
		t.Fatal("specialization modified the original candidate")
	}
}

func TestSpecializeIneligible(t *testing.T) {
	mc := specializeFixture(t)
	got, err := SpecializeDistribution(mc, &platform.Facts{OS: "freebsd", TargetArch: "x86_64", TargetFamily: "unix"})
	if err != nil || got != nil {
		t.Fatalf("no match must be ineligible (nil, nil), got %v, %v", got, err)
	}
	gated := cloneMethod(mc)
	gated.When = &Condition{OS: []string{"darwin"}}
	got, err = SpecializeDistribution(gated, linuxGlibc())
	if err != nil || got != nil {
		t.Fatalf("method-level when mismatch must be ineligible, got %v, %v", got, err)
	}
}

func TestSpecializeAmbiguous(t *testing.T) {
	s, err := ParseProjectSchema(writeSchema(t, `
[tools.x.github]
repo = "o/r"
asset = "base.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.x.github.distributions]]
when = { os = ["linux"] }

[[tools.x.github.distributions]]
when = { arch = ["x86_64"] }
`), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = SpecializeDistribution(s.Tools["x"].Methods[0], linuxGlibc())
	if err == nil || !strings.Contains(err.Error(), "2 distributions match") {
		t.Fatalf("ambiguity must abort with count, got %v", err)
	}
}

func TestSpecializeRegexAssetForm(t *testing.T) {
	s, err := ParseProjectSchema(writeSchema(t, `
[tools.atlas.github]
repo = "example/atlas"
asset = "atlas-{os_any}-{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"

[[tools.atlas.github.distributions]]
when = { os = ["linux"] }
asset = { regex = 'atlas-linux-.*\.tar\.gz' }
`), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := SpecializeDistribution(s.Tools["atlas"].Methods[0], linuxGlibc())
	if err != nil {
		t.Fatal(err)
	}
	form, ok := got.Config["asset"].(map[string]any)
	if !ok || form["regex"] != `atlas-linux-.*\.tar\.gz` {
		t.Fatalf("regex asset must materialize as map, got %v", got.Config["asset"])
	}
}
