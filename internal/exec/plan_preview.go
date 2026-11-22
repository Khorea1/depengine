package exec

import "github.com/Khorea1/depengine/internal/plan"

// annotateInstallPreview records what executing the install step will do. The
// argv lives in Operation.Preview, never in Operation.Command: a dry-run
// preview must not turn an adapter install into arbitrary code. A nil argv
// means the adapter cannot know the command before execution, so the step keeps
// an explicit unknown elevation decision instead of omitting it.
func annotateInstallPreview(resolved *plan.ResolvedInstallPlan, argv []string, elevation plan.ElevationDecision) {
	if resolved == nil {
		return
	}
	for i := range resolved.Operations {
		if resolved.Operations[i].Kind != "install" {
			continue
		}
		if len(argv) > 0 {
			resolved.Operations[i].Preview = append([]string(nil), argv...)
		}
		resolved.Operations[i].Elevation = elevation.Effective()
	}
}
