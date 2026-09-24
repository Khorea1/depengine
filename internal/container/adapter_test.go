package container

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
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

type authInspectRunner struct {
	nameAwareRunner
	env              map[string]string
	fileData         []byte
	filePath         string
	contextDataFound bool
	contextIsSymlink bool
	fileMode         os.FileMode
}

func (r *authInspectRunner) RunWithEnv(ctx context.Context, env map[string]string, _ []string, name string, args ...string) run.Result {
	r.env = env
	for _, key := range []string{"DOCKER_CONFIG", "REGISTRY_AUTH_FILE"} {
		if root := env[key]; root != "" {
			if key == "DOCKER_CONFIG" {
				r.filePath = filepath.Join(root, "config.json")
			} else {
				r.filePath = root
			}
			data, err := os.ReadFile(r.filePath)
			if err != nil {
				return run.Result{Err: err}
			}
			r.fileData = data
			info, err := os.Stat(r.filePath)
			if err != nil {
				return run.Result{Err: err}
			}
			r.fileMode = info.Mode().Perm()
			if key == "DOCKER_CONFIG" {
				_, err := os.Stat(filepath.Join(root, "contexts", "meta", "ctx", "meta.json"))
				r.contextDataFound = err == nil
				if info, err := os.Lstat(filepath.Join(root, "contexts")); err == nil {
					r.contextIsSymlink = info.Mode()&os.ModeSymlink != 0
				}
			}
		}
	}
	return r.Run(ctx, name, args...)
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

func TestContainerAdapterRegistryAuthIsScopedAndCleanedForEachManager(t *testing.T) {
	for _, tc := range []struct {
		manager, source, envKey, authKey string
	}{
		{"docker", "ghcr.io/acme/widget", "DOCKER_CONFIG", "ghcr.io"},
		{"podman", "quay.io/acme/widget", "REGISTRY_AUTH_FILE", "quay.io"},
		{"podman", "docker.io/acme/widget", "REGISTRY_AUTH_FILE", "docker.io"},
		{"docker", "acme/widget", "DOCKER_CONFIG", "https://index.docker.io/v1/"},
		{"docker", "busybox", "DOCKER_CONFIG", "https://index.docker.io/v1/"},
	} {
		t.Run(tc.manager+"/"+tc.source, func(t *testing.T) {
			// #nosec G101 -- synthetic test marker, never an actual credential.
			const username, credentialValue = "build-user", "credential-must-not-appear-in-argv"
			rn := &authInspectRunner{nameAwareRunner: nameAwareRunner{exitByName: map[string]int{tc.manager: 0}}}
			ctx := exec.WithContainerRegistryCredential(context.Background(), username, credentialValue)
			mc := &config.MethodCandidate{Config: map[string]any{"manager": tc.manager, "source": tc.source}, SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_SECRET"}}
			if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err != nil {
				t.Fatalf("Install: %v", err)
			}
			if len(rn.env) != 2 || rn.env[tc.envKey] == "" || rn.env["REGISTRY_SECRET"] != "" {
				t.Fatalf("environment overrides = %#v, want auth path plus blanked secret source", rn.env)
			}
			var config registryAuthConfig
			if err := json.Unmarshal(rn.fileData, &config); err != nil {
				t.Fatalf("decode auth file: %v", err)
			}
			if len(config.Auths) != 1 {
				t.Fatalf("auth entries = %#v, want exactly one registry entry", config.Auths)
			}
			entry, ok := config.Auths[tc.authKey]
			decodedAuth, decodeErr := base64.StdEncoding.DecodeString(entry.Auth)
			if !ok || decodeErr != nil || string(decodedAuth) != username+":"+credentialValue {
				t.Fatalf("auth file entries = %#v, want key %q decoding to username:secret", config.Auths, tc.authKey)
			}
			if rn.fileMode != 0o600 {
				t.Fatalf("auth file mode = %#o, want 0600", rn.fileMode)
			}
			call := rn.calls[len(rn.calls)-1]
			joinedArgs := strings.Join(call.Args, " ")
			if strings.Contains(joinedArgs, username) || strings.Contains(joinedArgs, credentialValue) {
				t.Fatalf("credential leaked into argv: %#v", call.Args)
			}
			if _, err := os.Stat(rn.filePath); !os.IsNotExist(err) {
				t.Fatalf("temporary auth file still exists (stat err %v)", err)
			}
		})
	}
}

func TestContainerAdapterRegistryAuthFailsClosedWithoutEnvironmentRunner(t *testing.T) {
	// #nosec G101 -- synthetic test marker, never an actual credential.
	const credentialValue = "credential-must-not-appear-in-argv"
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	ctx := exec.WithContainerRegistryCredential(context.Background(), "user", credentialValue)
	mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker", "source": "registry.example/acme/widget"}}
	if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err == nil || !strings.Contains(err.Error(), "per-call environment") {
		t.Fatalf("Install error = %v, want fail-closed environment-runner error", err)
	}
	if len(rn.calls) != 0 {
		t.Fatalf("runner without environment support executed a command: %#v", rn.calls)
	}
}

func TestContainerAdapterAuthenticatedPodmanRequiresExplicitRegistry(t *testing.T) {
	rn := &authInspectRunner{nameAwareRunner: nameAwareRunner{exitByName: map[string]int{"podman": 0}}}
	ctx := exec.WithContainerRegistryCredential(context.Background(), "user", "credential-value")
	mc := &config.MethodCandidate{
		Config:    map[string]any{"manager": "podman", "source": "team/widget"},
		SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_SECRET"},
	}
	if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err == nil || !strings.Contains(err.Error(), "explicit registry") {
		t.Fatalf("Install error = %v, want explicit registry requirement", err)
	}
	if len(rn.calls) != 0 {
		t.Fatalf("ambiguous Podman source reached runner: %#v", rn.calls)
	}
}

func TestContainerAdapterRegistryAuthFailureCleansTemporaryFile(t *testing.T) {
	rn := &authInspectRunner{nameAwareRunner: nameAwareRunner{exitByName: map[string]int{"docker": 1}}}
	ctx := exec.WithContainerRegistryCredential(context.Background(), "user", "secret")
	mc := &config.MethodCandidate{
		Config:    map[string]any{"manager": "docker", "source": "registry.example/acme/widget"},
		SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_SECRET"},
	}
	if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err == nil {
		t.Fatal("Install should report pull failure")
	}
	if rn.filePath == "" {
		t.Fatal("runner did not observe the temporary auth file")
	}
	if _, err := os.Stat(rn.filePath); !os.IsNotExist(err) {
		t.Fatalf("temporary auth file remains after failed pull (stat err %v)", err)
	}
}

func TestContainerAdapterRejectsInvalidRegistryUsername(t *testing.T) {
	for _, username := range []string{"user:name", "user\nname"} {
		t.Run(strings.ReplaceAll(username, "\n", "newline"), func(t *testing.T) {
			rn := &authInspectRunner{nameAwareRunner: nameAwareRunner{exitByName: map[string]int{"docker": 0}}}
			ctx := exec.WithContainerRegistryCredential(context.Background(), username, "secret")
			mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker", "source": "registry.example/acme/widget"}, SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_SECRET"}}
			if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err == nil || !strings.Contains(err.Error(), "invalid registry username") {
				t.Fatalf("Install error = %v, want invalid username error", err)
			}
			if len(rn.calls) != 0 {
				t.Fatalf("invalid username reached runner: %#v", rn.calls)
			}
		})
	}
}

func TestContainerAdapterDeclaredSecretRefFailsClosedWhenCredentialMissing(t *testing.T) {
	rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
	mc := &config.MethodCandidate{
		Config:    map[string]any{"manager": "docker", "source": "registry.example/acme/widget"},
		SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_TOKEN"},
	}
	if err := NewContainerAdapter().Install(context.Background(), rn, tool("widget"), mc); err == nil || !strings.Contains(err.Error(), "declared registry credential is unavailable") {
		t.Fatalf("Install error = %v, want missing declared credential error", err)
	}
	if len(rn.calls) != 0 {
		t.Fatalf("missing declared credential reached runner: %#v", rn.calls)
	}
}

func TestContainerAdapterSecretNameCannotCollideWithAuthOverride(t *testing.T) {
	for _, secretName := range []string{"DOCKER_CONFIG", "REGISTRY_AUTH_FILE"} {
		t.Run(secretName, func(t *testing.T) {
			rn := &nameAwareRunner{exitByName: map[string]int{"docker": 0}}
			ctx := exec.WithContainerRegistryCredential(context.Background(), "user", "secret")
			mc := &config.MethodCandidate{
				Config:    map[string]any{"manager": "docker", "source": "registry.example/acme/widget"},
				SecretRef: &config.SecretReference{Provider: "env", Name: secretName},
			}
			if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err == nil || !strings.Contains(err.Error(), "invalid registry secret environment name") {
				t.Fatalf("Install error = %v, want environment-name collision error", err)
			}
			if len(rn.calls) != 0 {
				t.Fatalf("colliding secret name reached runner: %#v", rn.calls)
			}
		})
	}
}

func TestContainerAdapterDockerAuthPreservesSelectedContext(t *testing.T) {
	configDir := t.TempDir()
	contextMeta := filepath.Join(configDir, "contexts", "meta", "ctx")
	if err := os.MkdirAll(contextMeta, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextMeta, "meta.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"currentContext":"ctx","auths":{"ambient.example":{"auth":"must-not-copy"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", configDir)
	rn := &authInspectRunner{nameAwareRunner: nameAwareRunner{exitByName: map[string]int{"docker": 0}}}
	ctx := exec.WithContainerRegistryCredential(context.Background(), "user", "secret")
	mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker", "source": "registry.example/acme/widget"}, SecretRef: &config.SecretReference{Provider: "env", Name: "REGISTRY_SECRET"}}
	if err := NewContainerAdapter().Install(ctx, rn, tool("widget"), mc); err != nil {
		t.Fatalf("Install: %v", err)
	}
	var authConfig registryAuthConfig
	if err := json.Unmarshal(rn.fileData, &authConfig); err != nil {
		t.Fatalf("decode generated Docker config: %v", err)
	}
	if authConfig.CurrentContext != "ctx" || !rn.contextDataFound {
		t.Fatalf("Docker context not preserved: currentContext=%q metadataFound=%v", authConfig.CurrentContext, rn.contextDataFound)
	}
	if rn.contextIsSymlink {
		t.Fatal("Docker contexts must be copied into the temporary configuration")
	}
	if len(authConfig.Auths) != 1 {
		t.Fatalf("generated auth entries = %#v, ambient auth must not be copied", authConfig.Auths)
	}
}

func TestContainerAdapterRejectsUnknownManager(t *testing.T) {
	for _, install := range []struct {
		name string
		call func(*nameAwareRunner, *config.MethodCandidate) error
	}{
		{"install", func(r *nameAwareRunner, mc *config.MethodCandidate) error {
			return NewContainerAdapter().Install(context.Background(), r, tool("widget"), mc)
		}},
		{"remove", func(r *nameAwareRunner, mc *config.MethodCandidate) error {
			return NewContainerAdapter().Remove(context.Background(), r, tool("widget"), mc)
		}},
	} {
		t.Run(install.name, func(t *testing.T) {
			rn := &nameAwareRunner{}
			mc := &config.MethodCandidate{Config: map[string]any{"manager": "docker;touch /tmp/pwned", "source": "registry.example/acme/widget"}}
			if err := install.call(rn, mc); err == nil {
				t.Fatal("unknown manager should be rejected")
			}
			if len(rn.calls) != 0 {
				t.Fatalf("unknown manager reached runner: %#v", rn.calls)
			}
		})
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
