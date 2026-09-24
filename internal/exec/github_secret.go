package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/secret"
)

// githubCredentialContext resolves an explicitly declared GitHub token and
// makes it available only through the request context. A declared reference
// is authoritative: resolution failure must not fall back to ambient tokens.
func (ex *Executor) githubCredentialContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil || method.Kind != "github" || method.SecretRef == nil {
		return ctx, nil
	}
	resolver := ex.secretResolver
	if resolver == nil {
		resolver = secret.EnvResolver{}
	}
	credential, err := resolver.Resolve(ctx, plan.SecretReference{
		Provider: method.SecretRef.Provider,
		Name:     method.SecretRef.Name,
	})
	if err != nil || credential == "" {
		return nil, fmt.Errorf("github secret %s", secretResolutionClass(err, credential))
	}
	return ghrelease.WithGithubToken(ctx, credential), nil
}

// executionCredentialContext resolves method-scoped credentials immediately
// before an already-resolved plan is executed. Credential values remain only
// in the returned context.
func (ex *Executor) executionCredentialContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil {
		return ctx, nil
	}
	ctx = run.WithOmittedEnv(ctx, methodSecretEnvNames(method)...)
	if method.Kind == "github" {
		return ex.githubCredentialContext(ctx, method)
	}
	if method.Kind == "git" {
		return ex.gitCredentialContext(ctx, method)
	}
	if method.Kind == "cargo" {
		return ex.cargoCredentialContext(ctx, method)
	}
	if method.Kind == "container" {
		return ex.containerCredentialContext(ctx, method)
	}
	switch method.Kind {
	case "http", "appimage", "android", "msi":
	default:
		return ctx, nil
	}
	resolver := ex.secretResolver
	if resolver == nil {
		resolver = secret.EnvResolver{}
	}
	for _, credentialRef := range httpCredentialReferences(method) {
		credential, err := resolver.Resolve(ctx, credentialRef.reference)
		if err != nil || credential == "" {
			return nil, fmt.Errorf("%s %s secret %s", method.Kind, credentialRef.purpose, secretResolutionClass(err, credential))
		}
		ctx = WithHTTPBearer(ctx, credentialRef.purpose, credential)
	}
	return ctx, nil
}

// InstallResolvedCandidate executes one concrete plan through the registered
// adapter while applying the same method-scoped credential transport used by
// normal installs.
func (ex *Executor) InstallResolvedCandidate(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if tool == nil || method == nil || resolved == nil {
		return fmt.Errorf("tool, method, and resolved plan are required")
	}
	adapter := ex.LookupAdapter(method.Kind)
	if adapter == nil {
		return fmt.Errorf("no adapter registered for %q", method.Kind)
	}
	execCtx, err := ex.executionCredentialContext(ctx, method)
	if err != nil {
		return err
	}
	return adapter.InstallResolved(execCtx, rn, tool, method, resolved)
}
