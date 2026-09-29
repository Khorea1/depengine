package ecosystem

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
)

// MasAdapter manages Mac App Store apps through the mas CLI. Install and check
// are delegated to the generic BaseAdapter; removal is specialized because
// `mas uninstall` requires root and accepts either an App Store app ID or bundle ID.
//
// UNVERIFIED on a real macOS host: the command shape follows the mas CLI
// documentation (`sudo mas uninstall <app-id>`), and behavior is covered only
// through a fake runner.
type MasAdapter struct {
	*BaseAdapter
}

// NewMasAdapter wraps the registry config for the "mas" kind.
func NewMasAdapter() *MasAdapter {
	return &MasAdapter{BaseAdapter: NewBaseAdapter(Configs["mas"])}
}

// CanRemove reports that mas apps can be uninstalled.
func (a *MasAdapter) CanRemove() bool { return true }

// RequiresRemovalElevation reports that mas uninstall requires root privileges.
func (a *MasAdapter) RequiresRemovalElevation(*config.Tool, *config.MethodCandidate) bool {
	return true
}

// Remove uninstalls the app with `mas uninstall <id>` through the elevation
// abstraction. Both numeric App Store IDs and reverse-DNS bundle IDs are accepted.
func (a *MasAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("mas: no package name")
	}
	id := strings.TrimSpace(pkg[0])
	if !isMasIdentifier(id) {
		return fmt.Errorf("mas: invalid App Store app ID or bundle ID %q", id)
	}
	if !a.Available(ctx, rn) {
		return fmt.Errorf("mas: mas binary not found")
	}
	res := run.RunElevated(ctx, rn, "mas", "uninstall", id)
	return run.CheckResult(res, "mas: remove")
}

var masIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)

func isMasIdentifier(s string) bool {
	return masIdentifierPattern.MatchString(s)
}

var _ exec.AdapterV2 = (*MasAdapter)(nil)
var _ exec.RemovalElevationRequirer = (*MasAdapter)(nil)
