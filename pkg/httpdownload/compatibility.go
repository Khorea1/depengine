package httpdownload

import (
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/plan"
)

var debArtifactExtensions = []string{".deb"}

// CheckHostCompatibility rejects distribution-specific download artifacts
// before they can be reported as installable on an unrelated host. HTTP as a
// transport is universally available, but a .deb payload is not a portable
// Linux binary merely because the CPU architecture matches.
func (a *HTTPAdapter) CheckHostCompatibility(tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan, facts *engine.Facts, clan string) error {
	return checkDownloadHostCompatibility(mc, intent, facts, clan)
}

// CheckHostCompatibility applies the same artifact policy to GitHub release
// assets. GitHubAdapter ultimately delegates installation to HTTPAdapter, so
// allowing the two methods to disagree here would make fallback/dry-run
// results depend on which transport spelling the schema used.
func (a *GitHubAdapter) CheckHostCompatibility(tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan, facts *engine.Facts, clan string) error {
	return checkDownloadHostCompatibility(mc, intent, facts, clan)
}

func checkDownloadHostCompatibility(mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan, facts *engine.Facts, clan string) error {
	if !candidateHasDebArtifact(mc, intent) {
		return nil
	}
	return checkDebHostCompatibility(mc, facts, clan)
}

func candidateHasDebArtifact(mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) bool {
	if intent != nil {
		for _, resolved := range intent.Artifacts {
			if artifact.Extension(resolved.URL, debArtifactExtensions) == ".deb" {
				return true
			}
		}
	}
	if mc == nil {
		return false
	}
	for _, key := range []string{"url", "asset"} {
		if raw, ok := mc.Config[key].(string); ok && artifact.Extension(raw, debArtifactExtensions) == ".deb" {
			return true
		}
	}
	return false
}

func checkDebHostCompatibility(mc *config.MethodCandidate, facts *engine.Facts, clan string) error {
	clan = strings.ToLower(strings.TrimSpace(clan))
	if clan == "" && facts != nil {
		clan = engine.ResolveFamily(facts)
	}

	// Debian-family distributions are the natural compatibility domain for a
	// generic .deb. Mint is kept as a distinct depengine clan for native-manager
	// metadata, but is Debian/Ubuntu-derived and consumes Debian packages.
	switch clan {
	case "debian", "mint":
		return nil
	}

	// A schema author may deliberately provide a target-built .deb for another
	// dpkg-capable environment (most importantly native Termux). Requiring an
	// exact distro/family condition makes that intent explicit instead of
	// treating the mere presence of apt/dpkg as Debian ABI compatibility.
	if explicitlyTargetsCurrentDistro(mc, facts, clan) {
		return nil
	}

	target := clan
	if target == "" {
		target = "unknown"
	}
	if target == "termux" {
		return fmt.Errorf(".deb artifact is not assumed Debian-compatible on native Termux; mark a Termux-built package explicitly with when.distro_family=[\"termux\"] or when.distro_id=[\"termux\"]")
	}
	return fmt.Errorf(".deb artifact is incompatible with distro family %q by default; use a Debian-family target or explicitly scope a package built for this distro with when.distro_family/when.distro_id", target)
}

func explicitlyTargetsCurrentDistro(mc *config.MethodCandidate, facts *engine.Facts, clan string) bool {
	if mc == nil || mc.When == nil {
		return false
	}
	for _, family := range mc.When.DistroFamily {
		if clan != "" && clan != "unknown" && strings.EqualFold(strings.TrimSpace(family), clan) {
			return true
		}
	}
	if facts == nil || strings.TrimSpace(facts.DistroID) == "" {
		return false
	}
	for _, id := range mc.When.DistroID {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(facts.DistroID)) {
			return true
		}
	}
	return false
}

var _ exec.HostCompatibilityChecker = (*HTTPAdapter)(nil)
var _ exec.HostCompatibilityChecker = (*GitHubAdapter)(nil)
