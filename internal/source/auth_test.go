package source

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

type authCommandRunner struct {
	name   string
	args   []string
	env    map[string]string
	result run.Result
}

func (r *authCommandRunner) Run(context.Context, string, ...string) run.Result {
	return run.Result{Err: context.Canceled}
}

func (r *authCommandRunner) RunWithEnv(_ context.Context, env map[string]string, _ []string, name string, args ...string) run.Result {
	r.name = name
	r.args = append([]string(nil), args...)
	r.env = env
	return r.result
}

func TestAuthenticatedSourcePassesBearerTokenOnlyToGitChild(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "")
	const token = "runtime-secret"
	source := config.Source{
		Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git",
		SecretRef: &config.SecretReference{Provider: "env", Name: "CORP_TOKEN"},
	}
	runner := &authCommandRunner{result: run.Result{Stderr: []byte(token), ExitCode: 1}}
	err := NewManager(runner, false).AddAuthenticated(context.Background(), source, token)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("source add error leaked credential: %v", err)
	}
	if runner.name != "brew" || len(runner.args) != 3 || runner.args[0] != "tap" || runner.args[2] != source.URL {
		t.Fatalf("source command = %s %v", runner.name, runner.args)
	}
	for _, arg := range runner.args {
		if strings.Contains(arg, token) {
			t.Fatal("token appeared in command arguments")
		}
	}
	if runner.env["GIT_CONFIG_COUNT"] != "1" || runner.env["GIT_CONFIG_KEY_0"] != "http."+source.URL+".extraheader" || runner.env["GIT_CONFIG_VALUE_0"] != "Authorization: Bearer "+token {
		t.Fatal("Git child did not receive URL-scoped bearer header")
	}
}

func TestAuthenticatedSourceRejectsMissingTransport(t *testing.T) {
	for _, source := range []config.Source{
		{Kind: "brew-tap", Name: "vendor/tools", SecretRef: &config.SecretReference{Provider: "env", Name: "TOKEN"}},
		{Kind: "brew-tap", Name: "vendor/tools", URL: "http://example.test/tools.git", SecretRef: &config.SecretReference{Provider: "env", Name: "TOKEN"}},
		{Kind: "apt-ppa", Name: "ppa:vendor/tools", SecretRef: &config.SecretReference{Provider: "env", Name: "TOKEN"}},
	} {
		runner := &authCommandRunner{}
		if err := NewManager(runner, false).AddAuthenticated(context.Background(), source, "token"); err == nil {
			t.Fatalf("unsupported authenticated source %+v was accepted", source)
		}
		if runner.name != "" {
			t.Fatalf("unsupported source reached command runner: %+v", source)
		}
	}
}

type sourceEnvProbeRunner struct{ executable string }

func (sourceEnvProbeRunner) Run(context.Context, string, ...string) run.Result {
	return run.Result{Err: context.Canceled}
}

func (r sourceEnvProbeRunner) RunWithEnv(ctx context.Context, env map[string]string, sensitive []string, _ string, _ ...string) run.Result {
	return (run.OSExecRunner{}).RunWithEnv(ctx, env, sensitive, r.executable, "-test.run=^TestAuthenticatedSourceChildEnvironment$")
}

func TestAuthenticatedSourceChildEnvironment(t *testing.T) {
	if os.Getenv("DEPENGINE_SOURCE_AUTH_CHILD") != "1" {
		return
	}
	if _, found := os.LookupEnv("DEPENGINE_SOURCE_AUTH_TOKEN"); found {
		t.Fatal("typed source secret variable reached child")
	}
	if os.Getenv("GIT_CONFIG_VALUE_0") != "Authorization: Bearer source-auth-marker" {
		t.Fatal("scoped Git credential did not reach source child")
	}
}

func TestAuthenticatedSourceDirectCallOmitsTypedSecretVariable(t *testing.T) {
	t.Setenv("DEPENGINE_SOURCE_AUTH_CHILD", "1")
	t.Setenv("DEPENGINE_SOURCE_AUTH_TOKEN", "source-auth-marker")
	t.Setenv("GIT_CONFIG_COUNT", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source := config.Source{
		Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git",
		SecretRef: &config.SecretReference{Provider: "env", Name: "DEPENGINE_SOURCE_AUTH_TOKEN"},
	}
	if err := NewManager(sourceEnvProbeRunner{executable: exe}, false).AddAuthenticated(context.Background(), source, "source-auth-marker"); err != nil {
		t.Fatal(err)
	}
}
