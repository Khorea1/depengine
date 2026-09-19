package container

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

// nameAwareRunner returns a canned exit code per binary name, so Available's
// "docker OR podman" branching can be exercised precisely — unlike
// run.FakeRunner, which returns the same exit code regardless of which
// binary was asked for.
type nameAwareRunner struct {
	exitByName map[string]int // missing entries default to exit 1 (not found)
	stdout     string
	calls      []run.FakeCall
}

func (r *nameAwareRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	// run.LookPath always invokes `which <binary>`, so the binary we care
	// about for Available() is args[0], not the invoked command name.
	key := name
	if name == "which" && len(args) > 0 {
		key = args[0]
	}
	code, ok := r.exitByName[key]
	if !ok {
		code = 1
	}
	return run.Result{Stdout: []byte(r.stdout), ExitCode: code}
}

func tool(name string) *config.Tool { return &config.Tool{Name: name} }

func TestContainerAdapterKind(t *testing.T) {
	if NewContainerAdapter().Kind() != "container" {
		t.Fatalf("Kind() = %q, want %q", NewContainerAdapter().Kind(), "container")
	}
}

func TestContainerAdapterAvailableDockerOnly(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	if !NewContainerAdapter().Available(context.Background(), rn) {
		t.Fatal("Available should be true when docker is on PATH")
	}
}

func TestContainerAdapterAvailablePodmanOnly(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if !NewContainerAdapter().Available(context.Background(), rn) {
		t.Fatal("Available should be true when podman is on PATH")
	}
}

func TestContainerAdapterAvailableNeitherFound(t *testing.T) {
	rn := &nameAwareRunner{}
	if NewContainerAdapter().Available(context.Background(), rn) {
		t.Fatal("Available should be false when neither docker nor podman is on PATH")
	}
}

func TestContainerAdapterCheckPresentImage(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "sha256:abc123\n"}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "podman", "source": "lscr.io/linuxserver/obsidian", "tag": "latest",
	}}

	if !NewContainerAdapter().Check(context.Background(), rn, tool("obsidian"), mc) {
		t.Fatal("Check should be true when `images -q` prints an image id")
	}
	last := rn.calls[len(rn.calls)-1]
	want := []string{"images", "-q", "lscr.io/linuxserver/obsidian:latest"}
	if last.Name != "podman" || !equalArgs(last.Args, want) {
		t.Fatalf("Check ran %v %v, want podman %v", last.Name, last.Args, want)
	}
}

func TestContainerAdapterCheckEmptyOutputMeansNotInstalled(t *testing.T) {
	// Exit 0 but no output: `images -q` found nothing matching the ref.
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}, stdout: ""}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis", "tag": "7",
	}}

	if NewContainerAdapter().Check(context.Background(), rn, tool("redis"), mc) {
		t.Fatal("Check should be false when `images -q` prints nothing, even with exit 0")
	}
}

func TestContainerAdapterCheckMissingConfigReturnsFalse(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}, stdout: "abc123"}
	mc := &config.MethodCandidate{Config: map[string]any{}}

	if NewContainerAdapter().Check(context.Background(), rn, tool("x"), mc) {
		t.Fatal("Check should be false when manager/source are missing, without running anything")
	}
	if len(rn.calls) != 0 {
		t.Fatalf("Check should not invoke the runner with incomplete config, got calls: %v", rn.calls)
	}
}

func TestContainerAdapterInstallDefaultsTagToLatest(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis",
	}}

	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := rn.calls[len(rn.calls)-1]
	want := []string{"pull", "redis:latest"}
	if last.Name != "docker" || !equalArgs(last.Args, want) {
		t.Fatalf("Install ran %v %v, want docker %v", last.Name, last.Args, want)
	}
}

func TestContainerAdapterInstallMissingManagerErrors(t *testing.T) {
	rn := &nameAwareRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{"source": "redis"}}

	err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc)
	if err == nil {
		t.Fatal("Install should error when manager is missing")
	}
	if len(rn.calls) != 0 {
		t.Fatalf("Install should not invoke the runner without a manager, got calls: %v", rn.calls)
	}
}

func TestContainerAdapterInstallMissingSourceErrors(t *testing.T) {
	rn := &nameAwareRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker"}}

	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err == nil {
		t.Fatal("Install should error when source is missing")
	}
}

func TestContainerAdapterInstallCommandFailurePropagates(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 1}}
	mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker", "source": "redis"}}

	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err == nil {
		t.Fatal("Install should propagate a non-zero exit as an error")
	}
}

func TestContainerAdapterCanRemove(t *testing.T) {
	if !NewContainerAdapter().CanRemove() {
		t.Fatal("CanRemove should always be true")
	}
}

func TestContainerAdapterRemoveSuccess(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "podman", "source": "lscr.io/linuxserver/obsidian", "tag": "latest",
	}}

	if err := NewContainerAdapter().Remove(context.Background(), rn, tool("obsidian"), mc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := rn.calls[len(rn.calls)-1]
	want := []string{"rmi", "lscr.io/linuxserver/obsidian:latest"}
	if last.Name != "podman" || !equalArgs(last.Args, want) {
		t.Fatalf("Remove ran %v %v, want podman %v", last.Name, last.Args, want)
	}
}

func TestContainerAdapterRemoveMissingConfigErrors(t *testing.T) {
	rn := &nameAwareRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{}}

	if err := NewContainerAdapter().Remove(context.Background(), rn, tool("x"), mc); err == nil {
		t.Fatal("Remove should error when manager/source are missing")
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

func TestContainerAdapterDeclaredFieldsGovernRuntimeCommands(t *testing.T) {
	adapter := NewContainerAdapter()
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "podman",
		"source":  "registry.example.test/team/tool",
		"tag":     "1.2.3",
	}}

	checkRunner := &nameAwareRunner{
		exitByName: map[string]int{"podman": 0},
		stdout:     "sha256:deadbeef\n",
	}
	if !adapter.Check(context.Background(), checkRunner, tool("ignored-name"), mc) {
		t.Fatal("Check should find the configured image")
	}
	checkCall := checkRunner.calls[len(checkRunner.calls)-1]
	if checkCall.Name != "podman" || !equalArgs(checkCall.Args, []string{"images", "-q", "registry.example.test/team/tool:1.2.3"}) {
		t.Fatalf("Check ran %v %v; declared manager/source/tag were not all honored", checkCall.Name, checkCall.Args)
	}

	installRunner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.Install(context.Background(), installRunner, tool("ignored-name"), mc); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	installCall := installRunner.calls[len(installRunner.calls)-1]
	if installCall.Name != "podman" || !equalArgs(installCall.Args, []string{"pull", "registry.example.test/team/tool:1.2.3"}) {
		t.Fatalf("Install ran %v %v; declared manager/source/tag were not all honored", installCall.Name, installCall.Args)
	}

	removeRunner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.Remove(context.Background(), removeRunner, tool("ignored-name"), mc); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	removeCall := removeRunner.calls[len(removeRunner.calls)-1]
	if removeCall.Name != "podman" || !equalArgs(removeCall.Args, []string{"rmi", "registry.example.test/team/tool:1.2.3"}) {
		t.Fatalf("Remove ran %v %v; declared manager/source/tag were not all honored", removeCall.Name, removeCall.Args)
	}
}

func TestContainerAdapterRejectsTaggedSourceBeforeRunner(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker", "source": "redis:7"}}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err == nil {
		t.Fatal("Install should reject a tag embedded in source")
	}
	if len(rn.calls) != 0 {
		t.Fatalf("invalid container reference reached runner: %#v", rn.calls)
	}
}

func TestContainerAdapterAllowsRegistryPort(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "registry.example:5000/team/tool", "tag": "1.2.3",
	}}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("tool"), mc); err != nil {
		t.Fatalf("Install rejected valid registry port: %v", err)
	}
	last := rn.calls[len(rn.calls)-1]
	if !equalArgs(last.Args, []string{"pull", "registry.example:5000/team/tool:1.2.3"}) {
		t.Fatalf("Install args = %v", last.Args)
	}
}

func TestContainerAdapterUsesDigestIdentity(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "podman", "source": "ghcr.io/owner/tool", "digest": digest,
	}}
	adapter := NewContainerAdapter()

	checkRunner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if !adapter.Check(context.Background(), checkRunner, tool("tool"), mc) {
		t.Fatal("Check should use immutable digest identity")
	}
	if got := checkRunner.calls[len(checkRunner.calls)-1].Args; !equalArgs(got, []string{"image", "inspect", "ghcr.io/owner/tool@" + digest}) {
		t.Fatalf("Check argv = %v", got)
	}

	installRunner := &nameAwareRunner{exitByName: map[string]int{"podman": 0}}
	if err := adapter.Install(context.Background(), installRunner, tool("tool"), mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := installRunner.calls[len(installRunner.calls)-1].Args; !equalArgs(got, []string{"pull", "ghcr.io/owner/tool@" + digest}) {
		t.Fatalf("Install argv = %v", got)
	}
}

func TestContainerAdapterRejectsTagAndDigestTogether(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis", "tag": "7", "digest": digest,
	}}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err == nil {
		t.Fatal("Install should reject tag and digest together")
	}
	if len(rn.calls) != 0 {
		t.Fatalf("invalid identity should fail before runner, got %v", rn.calls)
	}
}

func TestContainerAdapterDigestCheckDoesNotFallBackToTagPresence(t *testing.T) {
	digest := "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis", "digest": digest,
	}}
	// The runner reports inspect failure. A tag-based `images -q` implementation
	// could incorrectly treat another redis image as satisfying the pin, so the
	// exact digest check must return false without issuing a tag-presence query.
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 1}, stdout: "sha256:some-other-image\n"}
	if NewContainerAdapter().Check(context.Background(), rn, tool("redis"), mc) {
		t.Fatal("Check must reject a missing exact digest even if another repository image could be present")
	}
	if len(rn.calls) != 1 || rn.calls[0].Name != "docker" || !equalArgs(rn.calls[0].Args, []string{"image", "inspect", "redis@" + digest}) {
		t.Fatalf("digest check calls = %#v", rn.calls)
	}
}

func TestContainerAdapterInstallUsesPlatform(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis", "tag": "7", "platform": "LINUX/ARM64/V8",
	}}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := rn.calls[len(rn.calls)-1]
	want := []string{"pull", "--platform", "linux/arm64/v8", "redis:7"}
	if last.Name != "docker" || !equalArgs(last.Args, want) {
		t.Fatalf("Install ran %v %v, want docker %v", last.Name, last.Args, want)
	}
}

func TestContainerAdapterCheckVerifiesPlatform(t *testing.T) {
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "podman", "source": "redis", "tag": "7", "platform": "linux/arm64/v8",
	}}
	match := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "linux/arm64/v8\n"}
	if !NewContainerAdapter().Check(context.Background(), match, tool("redis"), mc) {
		t.Fatal("Check should accept matching platform")
	}
	want := []string{"image", "inspect", "--format", "{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}", "redis:7"}
	if got := match.calls[len(match.calls)-1].Args; !equalArgs(got, want) {
		t.Fatalf("Check argv = %v, want %v", got, want)
	}

	drift := &nameAwareRunner{exitByName: map[string]int{"podman": 0}, stdout: "linux/amd64\n"}
	if NewContainerAdapter().Check(context.Background(), drift, tool("redis"), mc) {
		t.Fatal("Check should reject platform drift")
	}
}

func TestContainerAdapterRejectsInvalidPlatformBeforeRunner(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{Config: map[string]any{
		"manager": "docker", "source": "redis", "platform": "linux",
	}}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("redis"), mc); err == nil {
		t.Fatal("Install should reject invalid platform")
	}
	if len(rn.calls) != 0 {
		t.Fatalf("invalid platform reached runner: %#v", rn.calls)
	}
}
