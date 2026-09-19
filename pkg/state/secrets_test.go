package state

import (
	"strings"
	"testing"
)

func TestValidateNoSecretsRejectsSensitiveConfigKeys(t *testing.T) {
	tests := []string{"token", "auth_token", "access-token", "password", "secret", "api_key", "Authorization", "cookie"}
	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			st := &State{Tools: map[string]ToolState{
				"tool": {Config: map[string]any{key: "do-not-persist"}},
			}}
			err := ValidateNoSecrets(st)
			if err == nil {
				t.Fatal("expected sensitive field to be rejected")
			}
			if strings.Contains(err.Error(), "do-not-persist") {
				t.Fatalf("error echoed secret value: %q", err)
			}
		})
	}
}

func TestValidateNoSecretsRejectsCredentialURL(t *testing.T) {
	st := &State{Tools: map[string]ToolState{
		"tool": {Config: map[string]any{"url": "https://alice:supersecret@example.com/file"}},
	}}
	err := ValidateNoSecrets(st)
	if err == nil {
		t.Fatal("expected credential URL to be rejected")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error echoed URL credential: %q", err)
	}
}

func TestValidateNoSecretsAllowsOrdinaryMethodConfig(t *testing.T) {
	st := &State{Tools: map[string]ToolState{
		"tool": {Config: map[string]any{
			"url":         "https://example.com/file.tar.gz",
			"checksum":    "sha256:abc",
			"signing_key": "ABCD1234",
			"entrypoints": map[string]any{"tool": "bin/tool"},
		}},
	}}
	if err := ValidateNoSecrets(st); err != nil {
		t.Fatalf("ordinary config rejected: %v", err)
	}
}

func TestValidateNoSecretsRejectsCredentialBearingCommand(t *testing.T) {
	tests := []any{
		"curl --token supersecret https://example.com",
		[]any{"curl", "--token", "supersecret", "https://example.com"},
		"Authorization: Bearer supersecret",
	}
	for i, command := range tests {
		st := &State{Tools: map[string]ToolState{
			"tool": {Config: map[string]any{"build": command}},
		}}
		err := ValidateNoSecrets(st)
		if err == nil {
			t.Fatalf("case %d: expected credential-bearing command to be rejected", i)
		}
		if strings.Contains(err.Error(), "supersecret") {
			t.Fatalf("case %d: error echoed secret: %q", i, err)
		}
	}
}
