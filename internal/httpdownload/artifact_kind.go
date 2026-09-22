package httpdownload

import (
	"fmt"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
)

// requireMethodArtifact enforces the same method-specific artifact contract
// used by semantic validation. CLI flows normally catch this before execution,
// but adapters are public package APIs and must reject invalid payloads when
// invoked directly as well.
func requireMethodArtifact(kind string, mc *config.MethodCandidate) error {
	if mc == nil {
		return fmt.Errorf("%s: missing method configuration", kind)
	}
	contract, ok := methodkind.Lookup(kind)
	if !ok || contract.Artifact == nil {
		return nil
	}
	raw, _ := mc.Config["url"].(string)
	if raw == "" {
		raw, _ = mc.Config["asset"].(string)
	}
	if raw == "" {
		return fmt.Errorf("%s: missing artifact source", kind)
	}
	if err := contract.Artifact.ValidateArtifact(raw); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}
