package exec

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/secret"
)

type containerRegistryCredential struct {
	username string
	secret   string
}

type containerRegistryCredentialKey struct{}

// WithContainerRegistryCredential carries one registry credential only for the
// reached container pull operation.
func WithContainerRegistryCredential(ctx context.Context, username, secretValue string) context.Context {
	return context.WithValue(ctx, containerRegistryCredentialKey{}, containerRegistryCredential{username: username, secret: secretValue})
}

// ContainerRegistryCredential returns the ephemeral username/secret pair for
// the current container pull.
func ContainerRegistryCredential(ctx context.Context) (username, secretValue string, ok bool) {
	credential, ok := ctx.Value(containerRegistryCredentialKey{}).(containerRegistryCredential)
	if !ok || credential.username == "" || credential.secret == "" {
		return "", "", false
	}
	return credential.username, credential.secret, true
}

func (ex *Executor) containerCredentialContext(ctx context.Context, method *config.MethodCandidate) (context.Context, error) {
	if method == nil || method.Kind != "container" || method.SecretRef == nil {
		return ctx, nil
	}
	username, _ := method.Config["auth_username"].(string)
	if username == "" || username != strings.TrimSpace(username) || strings.ContainsRune(username, ':') || strings.IndexFunc(username, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("container secret_ref requires a valid auth_username without colons or control characters")
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
		return nil, fmt.Errorf("container registry secret %s", secretResolutionClass(err, credential))
	}
	return WithContainerRegistryCredential(ctx, username, credential), nil
}
