package ecosystem

import (
	"context"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/config"
)

// GoAdapter extends BaseAdapter with a smarter Check that falls back to
// tool.Name (binary name) when `which {pkg}` fails. This handles tools
// where the Go import path differs from the binary name, e.g.
//
//	fzf = { go = "github.com/junegunn/fzf" }
//
// which installs a binary called "fzf", not "github.com/junegunn/fzf".
type GoAdapter struct {
	*BaseAdapter
}

// NewGoAdapter creates a Go adapter with import-aware Check.
func NewGoAdapter() *GoAdapter {
	return &GoAdapter{
		BaseAdapter: NewBaseAdapter(Configs["go"]),
	}
}

// Check runs `which {pkg}` (the import path); if that fails, it retries
// with `which tool.Name` (the binary name that `go install` produces).
func (a *GoAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	// First try: which {pkg} (import path or explicit pkg config).
	if a.BaseAdapter.Check(ctx, rn, tool, mc) {
		return true
	}

	// Fallback: which tool.Name — go install produces a binary named
	// after the last path element of the import path.
	// Only try if tool.Name differs from {pkg}.
	importPath := tool.Name
	if p, ok := mc.Config["pkg"].(string); ok && p != "" {
		importPath = p
	}
	if importPath != tool.Name {
		fallbackMC := &config.MethodCandidate{
			Kind:   mc.Kind,
			Config: map[string]any{"pkg": tool.Name},
		}
		return a.BaseAdapter.Check(ctx, rn, tool, fallbackMC)
	}
	return false
}

// Ensure GoAdapter implements exec.Adapter.
var _ exec.Adapter = (*GoAdapter)(nil)
