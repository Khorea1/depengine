package exec

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestReportJSONIncludesSelectedPlanIntent(t *testing.T) {
	p := plan.New("demo", "native", true)
	r := &ExecReport{Tools: []ToolResult{{Tool: "demo", Status: StatusWouldInstall, Method: "native", PlanIntent: &p}}}
	if got := r.JSON(); !strings.Contains(got, `"plan_intent"`) || !strings.Contains(got, `"plan_version": 1`) {
		t.Fatalf("JSON() = %s", got)
	}
}
