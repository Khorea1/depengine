package ecosystem

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
)

func tool(name, pkg string) (*config.Tool, *config.MethodCandidate) {
	t := &config.Tool{Name: name}
	mc := &config.MethodCandidate{
		Kind:   name,
		Config: map[string]any{"pkg": pkg},
	}
	return t, mc
}

func TestBaseAdapterAvailable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-avail",
		Binary:   executable,
	})

	if !adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should be true for 'sh'")
	}
	if adapter.Kind() != "test-avail" {
		t.Fatalf("Kind() = %q, want 'test-avail'", adapter.Kind())
	}
}

func TestSnapTypedOptionsArgv(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["snap"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "nvim", "confinement": "classic", "channel": "beta"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "nvim"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	if got != "install nvim --classic --channel=beta" {
		t.Fatalf("argv=%q", got)
	}
}

func TestBaseAdapterAvailableMissing(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-missing",
		Binary:   "this-binary-does-not-exist-hopefully",
	})

	if adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should be false for nonexistent binary")
	}
}

func TestBaseAdapterAvailableExtra(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:       "test-extra",
		Binary:         "this-does-not-exist",
		AvailableExtra: executable,
	})

	if !adapter.Available(context.Background(), run.OSExecRunner{}) {
		t.Fatal("Available should fall back to the extra binary")
	}
}

func TestBaseAdapterCheckWithRunner(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:  "test-check",
		Binary:    "sh",
		CheckTmpl: []string{"test", "-f", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true when exit code 0")
	}
}

func TestBaseAdapterCheckFails(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:  "test-check-fail",
		Binary:    "sh",
		CheckTmpl: []string{"test", "-f", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	if adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be false when exit code non-zero")
	}
}

func TestBaseAdapterInstallSuccess(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "test-install",
		Binary:      "sh",
		InstallTmpl: []string{"echo", "install", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBaseAdapterInstallFailure(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "error: permission denied"}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:    "test-install-fail",
		Binary:      "sh",
		InstallTmpl: []string{"false"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBaseAdapterCanRemoveFalseWhenEmpty(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-no-remove",
		Binary:   "sh",
	})
	if adapter.CanRemove() {
		t.Fatal("CanRemove should be false when RemoveTmpl is empty")
	}
}

func TestBaseAdapterCanRemoveTrueWhenSet(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-has-remove",
		Binary:     "sh",
		RemoveTmpl: []string{"echo", "remove", "{pkg}"},
	})
	if !adapter.CanRemove() {
		t.Fatal("CanRemove should be true when RemoveTmpl is set")
	}
}
func TestBaseAdapterRemoveSuccess(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-remove",
		Binary:     "sh",
		RemoveTmpl: []string{"echo", "remove", "{pkg}"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBaseAdapterRemoveNoCommand(t *testing.T) {
	adapter := NewBaseAdapter(BaseConfig{
		KindName: "test-remove-empty",
		Binary:   "sh",
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), &run.FakeRunner{}, tl, mc)
	if err == nil {
		t.Fatal("expected error when no remove command, got nil")
	}
}

func TestBaseAdapterRemoveFailure(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "error: not installed"}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:   "test-remove-fail",
		Binary:     "sh",
		RemoveTmpl: []string{"false"},
	})
	tl, mc := tool("test-tool", "test-pkg")

	err := adapter.Remove(context.Background(), fr, tl, mc)
	if err == nil {
		t.Fatal("expected error on non-zero exit, got nil")
	}
}

func TestAURAdapterAvailable(t *testing.T) {
	// We can't assume paru/yay exists, so use FakeRunner.
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewAURAdapter("paru")

	if !adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be true when which returns 0")
	}
}

func TestAURAdapterAvailableMissing(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewAURAdapter("nonexistent-aur-helper")

	if adapter.Available(context.Background(), fr) {
		t.Fatal("Available should be false when which returns non-zero")
	}
}

func TestAURAdapterCheckInstall(t *testing.T) {
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewAURAdapter("paru")
	tl, mc := tool("test-aur-pkg", "test-aur-pkg")

	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true with exit 0")
	}

	err := adapter.Install(context.Background(), fr, tl, mc)
	if err != nil {
		t.Fatalf("Install should succeed: %v", err)
	}
}

func TestSubstitutePkgFromConfig(t *testing.T) {
	tl := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "mycustompkg"}}

	got := exec.SubstitutePkg([]string{"{pkg}"}, tl, mc)
	if len(got) == 0 || got[0] != "mycustompkg" {
		t.Fatalf("SubstitutePkg = %v, want %q", got, "mycustompkg")
	}
}

func TestSubstitutePkgFallback(t *testing.T) {
	tl := &config.Tool{Name: "mytool"}
	mc := &config.MethodCandidate{Config: map[string]any{}}

	got := exec.SubstitutePkg([]string{"{pkg}"}, tl, mc)
	if len(got) == 0 || got[0] != "mytool" {
		t.Fatalf("SubstitutePkg fallback = %v, want %q", got, "mytool")
	}
}

// TestPipxCheckReturnsFalseForUninstalled verifies that parsing the direct
// pipx output does not confuse package-name prefixes with exact matches.
func TestPipxCheckReturnsFalseForUninstalled(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Stdout: "nonexistent-pkg-extra 1.0\n", ExitCode: 0}
	adapter := NewBaseAdapter(Configs["pipx"])

	tool := &config.Tool{Name: "nonexistent-pkg"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-pkg"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("pipx Check should return false for uninstalled package")
	}
}

// TestUvCheckReturnsFalseForUninstalled verifies the same exact-name rule for uv.
func TestUvCheckReturnsFalseForUninstalled(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{Stdout: "nonexistent-uv-tool-extra 1.0\n", ExitCode: 0}
	adapter := NewBaseAdapter(Configs["uv"])

	tool := &config.Tool{Name: "nonexistent-uv-tool"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "nonexistent-uv-tool"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("uv Check should return false for uninstalled tool")
	}
}

func TestRegistryChecksParseDirectCommandOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind   string
		pkg    string
		stdout string
		want   bool
	}{
		{"cargo", "bat", "bat v0.25.0:\n    bat\n", true},
		{"cargo", "bat", "bat-extra v1.0.0:\n", false},
		{"pipx", "black", `{"venvs":{"black":{"main_package":{"package":"black","package_version":"25.1.0"}}}}`, true},
		{"uv", "ruff", "ruff v0.11.0\n- ruff\n", true},
		{"bun", "typescript", "└── typescript@5.8.2\n", true},
		{"gem", "rake", "rake (13.2.1)\n", true},
		{"yarn", "typescript", "info \"typescript@5.8.2\" has binaries:\n", true},
		{"apm", "minimap", "minimap@4.40.0\n", true},
		{"vscode", "golang.go", "golang.go\nms-python.python\n", true},
		{"vscode", "golang.go", "vendor.golang.go-extra\n", false},
		{"vscodium", "golang.go", "golang.go\r\n", true},
		{"mas", "497799835", "497799835 Xcode (16.3)\n", true},
	}

	for _, tc := range tests {
		t.Run(tc.kind+"/"+tc.pkg, func(t *testing.T) {
			cfg := Configs[tc.kind]
			fr := &run.FakeRunner{Stdout: tc.stdout}
			tl, mc := tool(tc.pkg, tc.pkg)
			got := NewBaseAdapter(cfg).Check(context.Background(), fr, tl, mc)
			if got != tc.want {
				t.Fatalf("Check output %q = %v, want %v", tc.stdout, got, tc.want)
			}
			last := fr.Calls[len(fr.Calls)-1]
			if last.Name == "sh" || last.Name != cfg.CheckTmpl[0] {
				t.Fatalf("check command = %v %v, want direct %q", last.Name, last.Args, cfg.CheckTmpl[0])
			}
		})
	}
}

func TestParseMajorVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		want  int
		found bool
	}{
		{"1.22.19", 1, true},
		{"2.0.0", 2, true},
		{"3.1.0", 3, true},
		{"v2.1.0", 2, true},
		{"v10.0.1", 10, true},
		{"20.0.0", 20, true},
		{"berry-2.0.0", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseMajorVersion(tc.in)
			if ok != tc.found {
				t.Fatalf("parseMajorVersion(%q) found = %v, want %v", tc.in, ok, tc.found)
			}
			if ok && got != tc.want {
				t.Fatalf("parseMajorVersion(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestYarnBerryAvailableVersionGating(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stdout string
		want   bool
	}{
		{"classic yarn 1.x", "1.22.19\n", false},
		{"berry 2.x", "2.0.0\n", true},
		{"berry 3.x", "3.1.0\n", true},
		{"v-prefixed berry", "v2.1.0\n", true},
		{"multi-digit major", "10.0.0\n", true},
		{"non-numeric prefix", "berry-2.0.0\n", false},
		{"empty version", "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &run.FakeRunner{ExitCode: 0, Stdout: tc.stdout}
			adapter := NewYarnBerryAdapter()
			got := adapter.Available(context.Background(), fr)
			if got != tc.want {
				t.Fatalf("Available with stdout %q = %v, want %v", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestPacstallCheckUsesCorrectFlag(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewPacstallAdapter()
	tool := &config.Tool{Name: "test"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "foo"}}

	adapter.Check(context.Background(), fr, tool, mc)

	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	call := fr.Calls[0]
	if call.Name != "pacstall" {
		t.Fatalf("expected command 'pacstall', got %q", call.Name)
	}
	if len(call.Args) != 2 || call.Args[0] != "-Ci" || call.Args[1] != "foo" {
		t.Fatalf("expected args ['-Ci', 'foo'], got %v", call.Args)
	}
}

func TestSteamCMDCheckWithEmptyInstallDir(t *testing.T) {
	t.Parallel()
	adapter := NewSteamCMDAdapter()
	tool := &config.Tool{Name: "test"}

	t.Run("returns false when dir config is empty string", func(t *testing.T) {
		mcWithEmptyDir := &config.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": ""},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithEmptyDir)
		if got {
			t.Fatal("Check should return false when dir is empty string")
		}
	})

	t.Run("returns false when no pkg", func(t *testing.T) {
		mcNoPkg := &config.MethodCandidate{Config: map[string]any{}}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcNoPkg)
		if got {
			t.Fatal("Check should return false when pkg is missing")
		}
	})

	t.Run("uses explicit dir when provided", func(t *testing.T) {
		mcWithDir := &config.MethodCandidate{
			Config: map[string]any{"pkg": "730", "dir": "/tmp"},
		}
		fr := &run.FakeRunner{ExitCode: 0}
		got := adapter.Check(context.Background(), fr, tool, mcWithDir)
		if got {
			t.Fatal("Check should return false (always checks for updates)")
		}
	})
}

// TestRemovalMatrixRegistryKinds locks in the CanRemove matrix for the
// BaseAdapter-driven kinds in Configs. Kinds with a RemoveTmpl must be
// removable; kinds deliberately left manual must not.
func TestRemovalMatrixRegistryKinds(t *testing.T) {
	removable := []string{"cargo", "pip", "pipx", "uv", "npm", "pnpm", "bun", "gem", "yarn", "composer", "flatpak", "snap", "cask", "appman"}
	manual := []string{"go", "apm", "vscode", "vscodium", "mas"}

	for _, kind := range removable {
		t.Run(kind+"/removable", func(t *testing.T) {
			cfg, ok := Configs[kind]
			if !ok {
				t.Fatalf("Configs[%q] missing", kind)
			}
			if len(cfg.RemoveTmpl) == 0 {
				t.Fatalf("Configs[%q].RemoveTmpl is empty; kind should be removable", kind)
			}
			if !exec.CanRemove(NewBaseAdapter(cfg)) {
				t.Fatalf("kind %q should report CanRemove=true", kind)
			}
		})
	}

	for _, kind := range manual {
		t.Run(kind+"/manual", func(t *testing.T) {
			cfg, ok := Configs[kind]
			if !ok {
				t.Fatalf("Configs[%q] missing", kind)
			}
			if len(cfg.RemoveTmpl) != 0 {
				t.Fatalf("Configs[%q].RemoveTmpl should stay empty (manual removal)", kind)
			}
			if exec.CanRemove(NewBaseAdapter(cfg)) {
				t.Fatalf("kind %q should report CanRemove=false", kind)
			}
		})
	}
}

func TestAURAdapterCanRemove(t *testing.T) {
	t.Parallel()
	adapter := NewAURAdapter("paru")
	if !exec.CanRemove(adapter) {
		t.Fatal("AURAdapter should implement Remover with CanRemove=true")
	}
	// Named aliases embed AURAdapter and must inherit removal support.
	for _, alias := range []string{"paru", "yay"} {
		byName := &AURByNameAdapter{AURAdapter: NewAURAdapter(alias), name: alias}
		if !exec.CanRemove(byName) {
			t.Fatalf("AUR alias %q should implement Remover", alias)
		}
	}
}

func TestAURAdapterRemove(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewAURAdapter("paru")
	tl, mc := tool("test-aur-pkg", "test-aur-pkg")

	if err := adapter.Remove(context.Background(), fr, tl, mc); err != nil {
		t.Fatalf("Remove should succeed: %v", err)
	}
	if len(fr.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fr.Calls))
	}
	got := fr.Calls[0]
	if got.Name != "paru" || len(got.Args) != 3 || got.Args[0] != "-Rns" || got.Args[1] != "--noconfirm" || got.Args[2] != "test-aur-pkg" {
		t.Errorf("unexpected remove call: %v", got)
	}
}

func TestAURAdapterRemoveFailure(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1, Stderr: "target not found"}
	adapter := NewAURAdapter("yay")
	tl, mc := tool("test-aur-pkg", "test-aur-pkg")

	if err := adapter.Remove(context.Background(), fr, tl, mc); err == nil {
		t.Fatal("expected Remove error on non-zero exit, got nil")
	}
}

// TestGoBinaryNamePrefersCmdElement locks in the derivation rule used by the
// go Check (and the {bin} template placeholder): the binary name is the last
// path element, or the element right after a "/cmd/" segment when present.
func TestGoBinaryNamePrefersCmdElement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		importPath string
		want       string
	}{
		{"golang.org/x/tools/cmd/stringer", "stringer"},
		{"golang.org/x/tools/cmd/goimports", "goimports"},
		{"github.com/foo/bar/cmd/baz", "baz"},
		// Element after /cmd/ wins even when the import path nests deeper.
		{"github.com/foo/cmd/tool/extra", "tool"},
		{"github.com/junegunn/fzf", "fzf"},
		{"k8s.io/kubectl", "kubectl"},
		{"simple", "simple"},
	}
	for _, tc := range cases {
		t.Run(tc.importPath, func(t *testing.T) {
			if got := goBinaryName(tc.importPath); got != tc.want {
				t.Fatalf("goBinaryName(%q) = %q, want %q", tc.importPath, got, tc.want)
			}
		})
	}
}

// TestGoAdapterCheckUsesDerivedBinaryName verifies that the go Check targets
// the binary produced by `go install <import path>` (which stringer), never
// the import path itself (which golang.org/x/tools/cmd/stringer always
// fails — the import path is not a PATH entry).
func TestGoAdapterCheckUsesDerivedBinaryName(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if !adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("Check should report installed when the derived binary exists")
	}
	last := fr.Calls[len(fr.Calls)-1]
	if last.Name != "which" || len(last.Args) != 1 || last.Args[0] != "stringer" {
		t.Fatalf("Check lookup = %v %v, want stringer", last.Name, last.Args)
	}
}

// TestGoAdapterCheckNeverChecksImportPath verifies the acceptance criterion
// "no binary-name lookup on import paths anywhere": every check invocation
// must target a bare binary name (no "/" in the positional argument).
func TestGoAdapterCheckNeverChecksImportPath(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 1}
	adapter := NewGoAdapter()
	tool := &config.Tool{Name: "gostr"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "golang.org/x/tools/cmd/stringer"}}

	if adapter.Check(context.Background(), fr, tool, mc) {
		t.Fatal("Check should be false when the derived binary is missing")
	}
	if len(fr.Calls) == 0 {
		t.Fatal("expected at least one check call")
	}
	for _, call := range fr.Calls {
		name := call.Args[len(call.Args)-1]
		if strings.Contains(name, "/") {
			t.Fatalf("Check ran a lookup on a path-style name %q (import paths must never be checked)... %v %v", name, call.Name, call.Args)
		}
	}
}

// TestBaseAdapterCheckSubstitutesBinPlaceholder verifies the {bin} template
// placeholder is rendered with the binary name derived from {pkg}.
func TestBaseAdapterCheckSubstitutesBinPlaceholder(t *testing.T) {
	t.Parallel()
	fr := &run.FakeRunner{ExitCode: 0}
	adapter := NewBaseAdapter(BaseConfig{
		KindName:  "go",
		Binary:    "go",
		CheckTmpl: []string{"which", "{bin}"},
	})
	tl, mc := tool("gostr", "golang.org/x/tools/cmd/stringer")

	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true when the derived binary exists")
	}
	last := fr.Calls[len(fr.Calls)-1]
	if last.Name != "which" || len(last.Args) != 1 || last.Args[0] != "stringer" {
		t.Fatalf("Check ran %v %v, want which stringer", last.Name, last.Args)
	}
}

// TestAppmanConfigCommands locks in the exact command shape for the appman
// (AM/AppMan AppImage manager) adapter: install and remove must be
// non-interactive (-y -i / -R), and check must be a native PATH lookup on the
// package name, since appman has no documented single-package
// "is this installed?" query.
func TestAppmanConfigCommands(t *testing.T) {
	t.Parallel()
	cfg, ok := Configs["appman"]
	if !ok {
		t.Fatal("Configs[\"appman\"] missing")
	}
	if cfg.Binary != "appman" {
		t.Fatalf("Binary = %q, want appman", cfg.Binary)
	}

	adapter := NewBaseAdapter(cfg)
	tl, mc := tool("obsidian", "obsidian")

	fr := &run.FakeRunner{ExitCode: 0}
	if !adapter.Check(context.Background(), fr, tl, mc) {
		t.Fatal("Check should be true when exit code 0")
	}
	last := fr.Calls[len(fr.Calls)-1]
	if last.Name != "which" || last.Args[len(last.Args)-1] != "obsidian" {
		t.Fatalf("Check lookup = %v %v, want obsidian", last.Name, last.Args)
	}

	fr = &run.FakeRunner{ExitCode: 0}
	if err := adapter.Install(context.Background(), fr, tl, mc); err != nil {
		t.Fatalf("Install: unexpected error: %v", err)
	}
	last = fr.Calls[len(fr.Calls)-1]
	wantInstall := []string{"-y", "-i", "obsidian"}
	if last.Name != "appman" || !equalArgs(last.Args, wantInstall) {
		t.Fatalf("Install ran %v %v, want appman %v", last.Name, last.Args, wantInstall)
	}

	if !adapter.CanRemove() {
		t.Fatal("CanRemove should be true (RemoveTmpl is set)")
	}
	fr = &run.FakeRunner{ExitCode: 0}
	if err := adapter.Remove(context.Background(), fr, tl, mc); err != nil {
		t.Fatalf("Remove: unexpected error: %v", err)
	}
	last = fr.Calls[len(fr.Calls)-1]
	wantRemove := []string{"-R", "obsidian"}
	if last.Name != "appman" || !equalArgs(last.Args, wantRemove) {
		t.Fatalf("Remove ran %v %v, want appman %v", last.Name, last.Args, wantRemove)
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSnapDeclaredFieldsGovernRuntimeCommands(t *testing.T) {
	adapter := NewBaseAdapter(Configs["snap"])
	tool := &config.Tool{Name: "tool-name-must-not-win"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg":         "actual-snap",
		"confinement": "devmode",
		"channel":     "edge",
	}}

	checkRunner := &run.FakeRunner{ExitCode: 0, Stdout: "Name Version Rev Tracking Publisher Notes\nactual-snap 1.0 42 latest/edge vendor -\n"}
	if !adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should succeed for configured snap package")
	}
	checkCall := checkRunner.Calls[len(checkRunner.Calls)-1]
	if checkCall.Name != "snap" || strings.Join(checkCall.Args, " ") != "list actual-snap" {
		t.Fatalf("Check call = %s %v, want snap list actual-snap", checkCall.Name, checkCall.Args)
	}

	installRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Install(context.Background(), installRunner, tool, mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	installCall := installRunner.Calls[len(installRunner.Calls)-1]
	if installCall.Name != "snap" || strings.Join(installCall.Args, " ") != "install actual-snap --devmode --channel=edge" {
		t.Fatalf("Install call = %s %v; declared pkg/confinement/channel were not all honored", installCall.Name, installCall.Args)
	}

	removeRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Remove(context.Background(), removeRunner, tool, mc); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	removeCall := removeRunner.Calls[len(removeRunner.Calls)-1]
	if removeCall.Name != "snap" || strings.Join(removeCall.Args, " ") != "remove actual-snap" {
		t.Fatalf("Remove call = %s %v, want snap remove actual-snap", removeCall.Name, removeCall.Args)
	}
}

func TestSnapExplicitDefaultOptionsDoNotInventFlags(t *testing.T) {
	adapter := NewBaseAdapter(Configs["snap"])
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg":         "actual-snap",
		"confinement": "strict",
		"channel":     "stable",
	}}
	fr := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "ignored"}, mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	call := fr.Calls[len(fr.Calls)-1]
	if got := strings.Join(call.Args, " "); got != "install actual-snap" {
		t.Fatalf("explicit snap defaults should use native defaults; argv=%q", got)
	}
}

func TestFlatpakPkgFieldGovernsRuntimeCommands(t *testing.T) {
	adapter := NewBaseAdapter(Configs["flatpak"])
	tool := &config.Tool{Name: "tool-name-must-not-win"}
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "com.example.Actual"}}

	checkRunner := &run.FakeRunner{ExitCode: 0}
	if !adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should succeed for configured flatpak package")
	}
	checkCall := checkRunner.Calls[len(checkRunner.Calls)-1]
	if checkCall.Name != "flatpak" || strings.Join(checkCall.Args, " ") != "info com.example.Actual" {
		t.Fatalf("Check call = %s %v, want flatpak info com.example.Actual", checkCall.Name, checkCall.Args)
	}

	installRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Install(context.Background(), installRunner, tool, mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	installCall := installRunner.Calls[len(installRunner.Calls)-1]
	if installCall.Name != "flatpak" || strings.Join(installCall.Args, " ") != "install -y com.example.Actual" {
		t.Fatalf("Install call = %s %v, want configured flatpak pkg", installCall.Name, installCall.Args)
	}

	removeRunner := &run.FakeRunner{ExitCode: 0}
	if err := adapter.Remove(context.Background(), removeRunner, tool, mc); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	removeCall := removeRunner.Calls[len(removeRunner.Calls)-1]
	if removeCall.Name != "flatpak" || strings.Join(removeCall.Args, " ") != "uninstall -y com.example.Actual" {
		t.Fatalf("Remove call = %s %v, want configured flatpak pkg", removeCall.Name, removeCall.Args)
	}
}

func TestFlatpakRemoteBranchAndScope(t *testing.T) {
	adapter := NewBaseAdapter(Configs["flatpak"])
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg":    "com.example.App",
		"remote": "corp",
		"branch": "stable",
		"scope":  "user",
	}}

	t.Run("check verifies origin and full ref", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "corp\n"}
		if !adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should accept matching origin")
		}
		call := fr.Calls[len(fr.Calls)-1]
		want := "info --user --show-origin com.example.App//stable"
		if call.Name != "flatpak" || strings.Join(call.Args, " ") != want {
			t.Fatalf("check=%s %v want flatpak %s", call.Name, call.Args, want)
		}

		fr = &run.FakeRunner{ExitCode: 0, Stdout: "flathub\n"}
		if adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should reject a package from the wrong remote")
		}
	})

	t.Run("install uses typed remote branch and scope", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		call := fr.Calls[len(fr.Calls)-1]
		want := "install -y --user corp com.example.App//stable"
		if call.Name != "flatpak" || strings.Join(call.Args, " ") != want {
			t.Fatalf("install=%s %v want flatpak %s", call.Name, call.Args, want)
		}
	})

	t.Run("remove uses same branch and scope identity", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		if err := adapter.Remove(context.Background(), fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		call := fr.Calls[len(fr.Calls)-1]
		want := "uninstall -y --user com.example.App//stable"
		if call.Name != "flatpak" || strings.Join(call.Args, " ") != want {
			t.Fatalf("remove=%s %v want flatpak %s", call.Name, call.Args, want)
		}
	})
}

func TestSnapStructuredChannelAndTrackingCheck(t *testing.T) {
	adapter := NewBaseAdapter(Configs["snap"])
	tool := &config.Tool{Name: "demo"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg":    "demo-snap",
		"track":  "2.0",
		"risk":   "candidate",
		"branch": "hotfix",
	}}

	t.Run("install composes track risk branch", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0}
		if err := adapter.Install(context.Background(), fr, tool, mc); err != nil {
			t.Fatal(err)
		}
		call := fr.Calls[len(fr.Calls)-1]
		want := "install demo-snap --channel=2.0/candidate/hotfix"
		if call.Name != "snap" || strings.Join(call.Args, " ") != want {
			t.Fatalf("install=%s %v want snap %s", call.Name, call.Args, want)
		}
	})

	t.Run("check verifies tracking channel", func(t *testing.T) {
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "Name Version Rev Tracking Publisher Notes\ndemo-snap 2.0 77 2.0/candidate/hotfix vendor -\n"}
		if !adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should accept matching tracking channel")
		}
		fr.Stdout = "Name Version Rev Tracking Publisher Notes\ndemo-snap 2.0 77 latest/stable vendor -\n"
		if adapter.Check(context.Background(), fr, tool, mc) {
			t.Fatal("Check should reject a different tracking channel")
		}
	})

	t.Run("risk shorthand verifies implicit latest track", func(t *testing.T) {
		short := &config.MethodCandidate{Config: map[string]any{"pkg": "demo-snap", "channel": "beta"}}
		fr := &run.FakeRunner{ExitCode: 0, Stdout: "Name Version Rev Tracking Publisher Notes\ndemo-snap 2.0 77 latest/beta vendor -\n"}
		if !adapter.Check(context.Background(), fr, tool, short) {
			t.Fatal("risk shorthand should match latest/beta tracking")
		}
	})
}

func TestPipExactVersionInstallAndCheck(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pip"])
	tool := &config.Tool{Name: "ruff"}
	mc := &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "ruff", "version": "0.13.1"}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"pip": true}}
	if err := adapter.Install(context.Background(), installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := installRunner.Calls[len(installRunner.Calls)-1]
	if got := strings.Join(append([]string{last.Name}, last.Args...), " "); got != "pip install ruff==0.13.1" {
		t.Fatalf("install argv = %q", got)
	}

	checkRunner := &run.FakeRunner{LookPaths: map[string]bool{"pip": true}, Stdout: "Name: ruff\nVersion: 0.13.1\n"}
	if !adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should accept the requested installed version")
	}
	checkRunner.Stdout = "Name: ruff\nVersion: 0.12.0\n"
	if adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should reject version drift")
	}
}

func TestPipInstalledVersion(t *testing.T) {
	adapter := NewBaseAdapter(Configs["pip"])
	fr := &run.FakeRunner{Stdout: "Name: ruff\nVersion: 0.13.1\n"}
	got, err := adapter.InstalledVersion(context.Background(), fr, &config.Tool{Name: "ruff"}, &config.MethodCandidate{Kind: "pip", Config: map[string]any{"pkg": "ruff"}})
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != "0.13.1" {
		t.Fatalf("InstalledVersion = %q, want 0.13.1", got)
	}
}

func TestNPMExactVersionInstallAndCheck(t *testing.T) {
	adapter := NewBaseAdapter(Configs["npm"])
	tool := &config.Tool{Name: "typescript"}
	mc := &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "typescript", "version": "5.9.2"}}

	installRunner := &run.FakeRunner{LookPaths: map[string]bool{"npm": true}}
	if err := adapter.Install(context.Background(), installRunner, tool, mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := installRunner.Calls[len(installRunner.Calls)-1]
	if got := strings.Join(append([]string{last.Name}, last.Args...), " "); got != "npm install -g typescript@5.9.2" {
		t.Fatalf("install argv = %q", got)
	}

	checkRunner := &run.FakeRunner{LookPaths: map[string]bool{"npm": true}, Stdout: `{"dependencies":{"typescript":{"version":"5.9.2"}}}`}
	if !adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should accept the requested installed version")
	}
	checkRunner.Stdout = `{"dependencies":{"typescript":{"version":"5.8.0"}}}`
	if adapter.Check(context.Background(), checkRunner, tool, mc) {
		t.Fatal("Check should reject version drift")
	}
}

func TestNPMInstalledVersion(t *testing.T) {
	adapter := NewBaseAdapter(Configs["npm"])
	fr := &run.FakeRunner{Stdout: `{"dependencies":{"typescript":{"version":"5.9.2"}}}`}
	got, err := adapter.InstalledVersion(context.Background(), fr, &config.Tool{Name: "typescript"}, &config.MethodCandidate{Kind: "npm", Config: map[string]any{"pkg": "typescript"}})
	if err != nil {
		t.Fatalf("InstalledVersion: %v", err)
	}
	if got != "5.9.2" {
		t.Fatalf("InstalledVersion = %q, want 5.9.2", got)
	}
}

func TestPipxTypedVersionScopeAndIndex(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["pipx"])
	mc := &config.MethodCandidate{Config: map[string]any{
		"pkg": "black", "version": "24.3.0", "scope": "global", "index_url": "https://packages.example/simple",
	}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "black"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "install --global --index-url https://packages.example/simple black==24.3.0"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestPipxCheckExactVersionAndScope(t *testing.T) {
	fr := &run.FakeRunner{Stdout: `{"pipx_spec_version":"0.1","venvs":{"black":{"main_package":{"package":"black","package_version":"24.3.0"}}}}`}
	adapter := NewBaseAdapter(Configs["pipx"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "black", "version": "24.3.0", "scope": "global"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "black"}, mc) {
		t.Fatal("expected exact pipx version to be satisfied")
	}
	last := fr.Calls[len(fr.Calls)-1]
	if got := strings.Join(last.Args, " "); got != "list --output json --global black" {
		t.Fatalf("check argv=%q", got)
	}
	mc.Config["version"] = "25.0.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "black"}, mc) {
		t.Fatal("version drift should not be satisfied")
	}
}

func TestPipxInstalledVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: `{"venvs":{"black":{"main_package":{"package":"black","package_version":"24.3.0"}}}}`}
	adapter := NewBaseAdapter(Configs["pipx"])
	got, err := adapter.InstalledVersion(context.Background(), fr, &config.Tool{Name: "black"}, &config.MethodCandidate{Config: map[string]any{"pkg": "black"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "24.3.0" {
		t.Fatalf("version=%q", got)
	}
}

func TestUVTypedVersionAndIndex(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["uv"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "ruff", "version": "0.11.0", "index": "https://packages.example/simple"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "ruff"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "tool install --index https://packages.example/simple ruff==0.11.0"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestUVCheckExactVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: "ruff v0.11.0\n- ruff\n"}
	adapter := NewBaseAdapter(Configs["uv"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "ruff", "version": "0.11.0"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "ruff"}, mc) {
		t.Fatal("expected exact uv version")
	}
	mc.Config["version"] = "0.12.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "ruff"}, mc) {
		t.Fatal("uv version drift should fail")
	}
}

func TestGemTypedVersionSourceAndScope(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["gem"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "rake", "version": "13.2.1", "source": "https://gems.example", "scope": "user"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "rake"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "install rake --version 13.2.1 --clear-sources --source https://gems.example --user-install"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestGemCheckExactInstalledVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: "rake (13.2.1, 13.1.0)\n"}
	adapter := NewBaseAdapter(Configs["gem"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "rake", "version": "13.1.0"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "rake"}, mc) {
		t.Fatal("requested installed gem version should satisfy")
	}
	mc.Config["version"] = "12.0.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "rake"}, mc) {
		t.Fatal("missing gem version should not satisfy")
	}
}

func TestPipTypedIndexURL(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["pip"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "black", "version": "24.3.0", "index_url": "https://packages.example/simple"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "black"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "install black==24.3.0 --index-url https://packages.example/simple"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestNPMTypedRegistry(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["npm"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2", "registry": "https://registry.example"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "typescript"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "install -g typescript@5.8.2 --registry https://registry.example"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestComposerTypedExactVersion(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["composer"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "phpstan/phpstan", "version": "1.12.0"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "phpstan"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "global require --no-interaction phpstan/phpstan:1.12.0"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestComposerCheckExactVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: "name     : phpstan/phpstan\nversions : * 1.12.0\n"}
	adapter := NewBaseAdapter(Configs["composer"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "phpstan/phpstan", "version": "1.12.0"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "phpstan"}, mc) {
		t.Fatal("expected composer version to satisfy")
	}
	mc.Config["version"] = "1.11.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "phpstan"}, mc) {
		t.Fatal("composer version drift should fail")
	}
}

func TestBunTypedVersionAndRegistry(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["bun"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2", "registry": "https://registry.example"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "typescript"}, mc); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " ")
	want := "add -g --registry https://registry.example typescript@5.8.2"
	if got != want {
		t.Fatalf("argv=%q want %q", got, want)
	}
}

func TestBunCheckExactVersionIncludingScopedPackage(t *testing.T) {
	fr := &run.FakeRunner{Stdout: "├── typescript@5.8.2\n└── @scope/tool@1.4.0\n"}
	adapter := NewBaseAdapter(Configs["bun"])
	for _, tc := range []struct{ pkg, version string }{{"typescript", "5.8.2"}, {"@scope/tool", "1.4.0"}} {
		mc := &config.MethodCandidate{Config: map[string]any{"pkg": tc.pkg, "version": tc.version}}
		if !adapter.Check(context.Background(), fr, &config.Tool{Name: tc.pkg}, mc) {
			t.Fatalf("expected %s@%s to satisfy", tc.pkg, tc.version)
		}
	}
}

func TestPNPMTypedExactVersion(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["pnpm"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "typescript"}, mc); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " "); got != "add -g typescript@5.8.2" {
		t.Fatalf("argv=%q", got)
	}
}

func TestPNPMCheckExactVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: `[{"dependencies":{"typescript":{"version":"5.8.2"}}}]`}
	adapter := NewBaseAdapter(Configs["pnpm"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "typescript"}, mc) {
		t.Fatal("expected pnpm exact version")
	}
	mc.Config["version"] = "5.7.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "typescript"}, mc) {
		t.Fatal("pnpm version drift should fail")
	}
}

func TestYarnTypedExactVersion(t *testing.T) {
	fr := &run.FakeRunner{}
	adapter := NewBaseAdapter(Configs["yarn"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2"}}
	if err := adapter.Install(context.Background(), fr, &config.Tool{Name: "typescript"}, mc); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fr.Calls[len(fr.Calls)-1].Args, " "); got != "global add typescript@5.8.2" {
		t.Fatalf("argv=%q", got)
	}
}

func TestYarnCheckExactVersion(t *testing.T) {
	fr := &run.FakeRunner{Stdout: `info "typescript@5.8.2" has binaries:` + "\n"}
	adapter := NewBaseAdapter(Configs["yarn"])
	mc := &config.MethodCandidate{Config: map[string]any{"pkg": "typescript", "version": "5.8.2"}}
	if !adapter.Check(context.Background(), fr, &config.Tool{Name: "typescript"}, mc) {
		t.Fatal("expected yarn exact version")
	}
	mc.Config["version"] = "5.7.0"
	if adapter.Check(context.Background(), fr, &config.Tool{Name: "typescript"}, mc) {
		t.Fatal("yarn version drift should fail")
	}
}
