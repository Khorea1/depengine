package exec

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/secret"
)

type gitCredentialKey struct{}

// WithGitCredential carries an ephemeral credential to one git adapter call.
func WithGitCredential(ctx context.Context, credential string) context.Context {
	return context.WithValue(ctx, gitCredentialKey{}, credential)
}

// GitCredential reads the credential assigned to the current git operation.
func GitCredential(ctx context.Context) (string, bool) {
	credential, ok := ctx.Value(gitCredentialKey{}).(string)
	return credential, ok && credential != ""
}

// gitCredentialContext resolves an explicitly declared git bearer token only
// when the candidate is about to execute. Static/read-only resolution never
// needs the token because git.secret_ref is not supported with {latest} URLs.
func (ex *Executor) gitCredentialContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil || method.Kind != "git" || method.SecretRef == nil {
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
		return nil, fmt.Errorf("git secret %s", secretResolutionClass(err, credential))
	}
	return WithGitCredential(ctx, credential), nil
}

// cargoCredentialContext resolves an explicitly declared token only when a
// Git-backed Cargo candidate is reached for execution.
func (ex *Executor) cargoCredentialContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil || method.Kind != "cargo" || method.SecretRef == nil {
		return ctx, nil
	}
	if _, ok := method.Config["git"].(string); !ok {
		return nil, fmt.Errorf("cargo secret requires a Git source")
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
		return nil, fmt.Errorf("cargo secret %s", secretResolutionClass(err, credential))
	}
	return WithGitCredential(ctx, credential), nil
}
