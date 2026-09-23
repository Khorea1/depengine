package secret

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/plan"
)

var (
	ErrInvalidReference    = errors.New("invalid secret reference")
	ErrUnsupportedProvider = errors.New("unsupported secret provider")
	ErrSecretMissing       = errors.New("secret is missing")
	ErrSecretEmpty         = errors.New("secret is empty")
)

// SecretResolver resolves a secret reference at runtime. Implementations must
// return secret material only to the immediate caller and must not persist it.
type SecretResolver interface {
	Resolve(context.Context, plan.SecretReference) (string, error)
}

// EnvResolver resolves provider "env" references from the process environment.
// Its zero value is ready for use.
type EnvResolver struct {
	lookupEnv func(string) (string, bool)
}

// Resolve returns the current environment value for ref. Secret material is
// never included in returned errors.
func (r EnvResolver) Resolve(_ context.Context, ref plan.SecretReference) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	if ref.Provider != "env" {
		return "", fmt.Errorf("%w %q", ErrUnsupportedProvider, ref.Provider)
	}

	lookup := r.lookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(ref.Name)
	if !ok {
		return "", fmt.Errorf("%w: env %q", ErrSecretMissing, ref.Name)
	}
	if value == "" {
		return "", fmt.Errorf("%w: env %q", ErrSecretEmpty, ref.Name)
	}
	return value, nil
}

var _ SecretResolver = EnvResolver{}
