package planner

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/localartifact"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

func applyArtifact(p *plan.ResolvedInstallPlan, method *config.MethodCandidate, contract *methodkind.Contract) error {
	checksum := stringValue(method.Config, "checksum")
	if err := contract.ValidateChecksum(checksum); err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	if _, declared := contract.Fields["local_path"]; declared {
		raw := stringValue(method.Config, "local_path")
		if raw == "" {
			return fmt.Errorf("local_path is required")
		}
		if matches := config.PlaceholderRe.FindAllString(raw, -1); len(matches) > 0 {
			return fmt.Errorf("local_path must be fully resolved before planning; unresolved placeholder(s): %s", strings.Join(matches, ", "))
		}
		localPath, err := plan.NormalizeProjectPath(raw)
		if err != nil {
			return err
		}
		kind, err := localartifact.ClassifyProjectPath(localPath)
		if err != nil {
			return err
		}
		p.Artifacts = append(p.Artifacts, plan.Artifact{
			Kind:      kind,
			LocalPath: localPath,
			Checksum:  checksum,
		})
		p.Operations = append(p.Operations, plan.Operation{
			Kind:        "resolve-local-artifact",
			Description: localPath,
			Effect:      plan.EffectReadOnly,
		})
		return nil
	}
	if contract.Artifact == nil {
		return nil
	}
	url := stringValue(method.Config, "url")
	if url == "" {
		if asset := stringValue(method.Config, "asset"); asset != "" {
			p.Operations = append(p.Operations, plan.Operation{Kind: "resolve-artifact", Description: asset, Effect: plan.EffectReadOnly})
		}
		return nil
	}
	p.Artifacts = append(p.Artifacts, plan.Artifact{
		URL:                url,
		Checksum:           checksum,
		ChecksumURL:        stringValue(method.Config, "checksum_url"),
		ChecksumFileFormat: stringValue(method.Config, "checksum_file_format"),
		SignatureURL:       stringValue(method.Config, "signature_url"),
		SigningKey:         stringValue(method.Config, "signing_key"),
	})
	return nil
}
