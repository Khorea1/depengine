package methodkind

import "github.com/Khorea1/depengine/internal/plan"

func artifactCapabilities(artifacts []plan.Artifact) Capability {
	for _, artifact := range artifacts {
		if artifact.LocalPath != "" {
			return CapabilityLocalArtifact
		}
	}
	return 0
}
