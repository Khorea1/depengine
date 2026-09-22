package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	depstate "github.com/Khorea1/depengine/internal/state"
)

// Versioner is an optional adapter interface for reporting the installed
// version while state is persisted.
type Versioner interface {
	Adapter
	InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, method *config.MethodCandidate) (string, error)
}

const versionProbeTimeout = 15 * time.Second

func (ex *Executor) installedVersion(ctx context.Context, tool *config.Tool, result ToolResult) string {
	adapter := ex.LookupAdapter(result.MethodKind)
	versioner, ok := adapter.(Versioner)
	if !ok {
		return ""
	}
	method := &config.MethodCandidate{Kind: result.MethodKind, Config: result.Config}
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	version, err := versioner.InstalledVersion(probeCtx, ex.rn, tool, method)
	if err != nil {
		return ""
	}
	return version
}

func (ex *Executor) writeState(ctx context.Context, schema *config.Schema, report *ExecReport) error {
	if ex.schemaPath == "" {
		ex.logWarn(ctx, "state not persisted: no schema path configured (install may not be trackable)")
		return nil
	}
	lockedState, err := depstate.LoadLocked()
	if err != nil {
		return fmt.Errorf("state lock failed: %w", err)
	}
	defer lockedState.Close()

	current := lockedState.State()
	current.SchemaPath = ex.schemaPath
	current.SchemaModifiedAt = ex.schemaModTime.UTC().Format(time.RFC3339)
	if current.Version == 0 {
		current.Version = depstate.CurrentVersion
	}
	if current.Tools == nil {
		current.Tools = make(map[string]depstate.ToolState, len(report.Tools))
	}
	for _, result := range report.Tools {
		if result.Status != StatusInstalled && result.Status != StatusAlready && !result.InstallCommitted {
			continue
		}
		tool, ok := schema.Tools[result.Tool]
		if !ok {
			continue
		}
		existing, hadExisting := current.Tools[result.Tool]
		toolState := depstate.ToolState{
			Method:          result.Method,
			MethodKind:      result.MethodKind,
			InstalledAt:     time.Now().UTC().Format(time.RFC3339),
			PostinstallDone: result.PostinstallDone,
			DefinitionHash:  depstate.DefinitionHash(tool),
			RootRequested:   !tool.DependencyOnly,
			Config:          result.Config,
		}
		// A successful Check means depengine did not install anything during this
		// run. Preserve historical installation metadata instead of rewriting the
		// original timestamp (and completed postinstall state) as though a fresh
		// installation had occurred. The definition/config are still refreshed so
		// state tracks the currently satisfied manifest intent.
		if result.Status == StatusAlready && hadExisting {
			if existing.InstalledAt != "" {
				toolState.InstalledAt = existing.InstalledAt
			}
			toolState.PostinstallDone = existing.PostinstallDone || result.PostinstallDone
		}
		// Direct/root intent is sticky until explicit removal/forget. A later run
		// that happens to reach the same tool only as a lazy dependency must not
		// silently make it eligible for prerequisite garbage collection.
		if hadExisting && existing.RootRequested {
			toolState.RootRequested = true
		}
		if version := ex.installedVersion(ctx, tool, result); version != "" {
			toolState.Version = version
		} else if hadExisting {
			toolState.Version = existing.Version
		}
		current.Tools[result.Tool] = toolState
		if len(result.ResourceUses) > 0 {
			owned, err := plan.ClaimResourceUses(current.OwnedResources, result.Tool, result.ResourceUses)
			if err != nil {
				return fmt.Errorf("claim resources for %s: %w", result.Tool, err)
			}
			current.OwnedResources = owned
		}
	}
	if err := ex.claimSchemaDependencyResources(current, schema, report); err != nil {
		return err
	}
	if err := lockedState.Save(); err != nil {
		return fmt.Errorf("state save failed: %w", err)
	}
	return nil
}

func (ex *Executor) claimSchemaDependencyResources(current *depstate.State, schema *config.Schema, report *ExecReport) error {
	committed := make(map[string]ToolResult, len(report.Tools))
	for _, result := range report.Tools {
		if result.Status == StatusInstalled || result.Status == StatusAlready || result.InstallCommitted {
			committed[result.Tool] = result
		}
	}

	for ownerName, ownerResult := range committed {
		owner := schema.Tools[ownerName]
		if owner == nil {
			continue
		}
		for _, dependencyName := range owner.EffectiveRequires(ex.facts) {
			if _, tracked := current.Tools[dependencyName]; !tracked {
				// Virtual dependency groups have no host resource to own.
				continue
			}
			resource, err := plan.PrerequisiteResource(dependencyName)
			if err != nil {
				return fmt.Errorf("claim dependency %s for %s: %w", dependencyName, ownerName, err)
			}
			dependencyResult, ran := committed[dependencyName]
			created := ran && (dependencyResult.Status == StatusInstalled || dependencyResult.InstallCommitted)
			owned, err := plan.ClaimResourceUses(current.OwnedResources, ownerName, []plan.ResourceUse{{
				Resource: resource,
				Created:  created,
			}})
			if err != nil {
				return fmt.Errorf("claim dependency resource %s for %s: %w", dependencyName, ownerResult.Tool, err)
			}
			current.OwnedResources = owned
		}
	}
	return nil
}
