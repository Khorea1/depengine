package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestEnvResolverResolveSuccess(t *testing.T) {
	const secretValue = "runtime-secret-value"
	t.Setenv("DEPENGINE_SECRET_RESOLVER_TEST_TOKEN", secretValue)

	got, err := (EnvResolver{}).Resolve(context.Background(), plan.SecretReference{Provider: "env", Name: "DEPENGINE_SECRET_RESOLVER_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != secretValue {
		t.Fatalf("Resolve() = %q, want secret value", got)
	}
}

func TestEnvResolverResolveMissingSecret(t *testing.T) {
	resolver := EnvResolver{lookupEnv: func(string) (string, bool) { return "ignored-secret", false }}

	_, err := resolver.Resolve(context.Background(), plan.SecretReference{Provider: "env", Name: "CORP_TOKEN"})
	if !errors.Is(err, ErrSecretMissing) {
		t.Fatalf("Resolve() error = %v, want ErrSecretMissing", err)
	}
	if got, want := err.Error(), `secret is missing: env "CORP_TOKEN"`; got != want {
		t.Fatalf("Resolve() error = %q, want %q", got, want)
	}
}

func TestEnvResolverResolveEmptySecret(t *testing.T) {
	resolver := EnvResolver{lookupEnv: func(string) (string, bool) { return "", true }}

	_, err := resolver.Resolve(context.Background(), plan.SecretReference{Provider: "env", Name: "CORP_TOKEN"})
	if !errors.Is(err, ErrSecretEmpty) {
		t.Fatalf("Resolve() error = %v, want ErrSecretEmpty", err)
	}
	if got, want := err.Error(), `secret is empty: env "CORP_TOKEN"`; got != want {
		t.Fatalf("Resolve() error = %q, want %q", got, want)
	}
}

func TestEnvResolverResolveUnsupportedProvider(t *testing.T) {
	resolver := EnvResolver{lookupEnv: func(string) (string, bool) {
		t.Fatal("unsupported provider must not read environment")
		return "", false
	}}

	_, err := resolver.Resolve(context.Background(), plan.SecretReference{Provider: "vault", Name: "CORP_TOKEN"})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedProvider", err)
	}
	if got, want := err.Error(), `unsupported secret provider "vault"`; got != want {
		t.Fatalf("Resolve() error = %q, want %q", got, want)
	}
}

func TestEnvResolverResolveInvalidReference(t *testing.T) {
	tests := []struct {
		name string
		ref  plan.SecretReference
	}{
		{name: "missing provider", ref: plan.SecretReference{Name: "TOKEN"}},
		{name: "missing name", ref: plan.SecretReference{Provider: "env"}},
		{name: "provider whitespace", ref: plan.SecretReference{Provider: " env", Name: "TOKEN"}},
		{name: "name whitespace", ref: plan.SecretReference{Provider: "env", Name: "TOKEN "}},
		{name: "provider nul", ref: plan.SecretReference{Provider: "env\x00", Name: "TOKEN"}},
		{name: "name nul", ref: plan.SecretReference{Provider: "env", Name: "TOK\x00EN"}},
	}

	resolver := EnvResolver{lookupEnv: func(string) (string, bool) {
		t.Fatal("invalid reference must not read environment")
		return "", false
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolver.Resolve(context.Background(), tt.ref)
			if !errors.Is(err, ErrInvalidReference) {
				t.Fatalf("Resolve() error = %v, want ErrInvalidReference", err)
			}
		})
	}
}

func TestEnvResolverErrorsDoNotLeakResolvedValue(t *testing.T) {
	const secretValue = "supersecret-do-not-leak"
	resolver := EnvResolver{lookupEnv: func(string) (string, bool) { return secretValue, false }}

	_, err := resolver.Resolve(context.Background(), plan.SecretReference{Provider: "env", Name: "CORP_TOKEN"})
	if err == nil {
		t.Fatal("Resolve() error = nil, want missing secret error")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("Resolve() error leaked secret value: %q", err)
	}
}
