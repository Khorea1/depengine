package source

import (
	"context"
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
