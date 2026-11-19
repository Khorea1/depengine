package httpdownload

import (
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// DescribeProspectiveEffects supplies partial write-target evidence for a
// concrete HTTP candidate. It does not change AdapterV2 or authorize effects.
// The envelope stays UNKNOWN, including when the main file slot is known:
// downloads, caches, staging files, hooks and other steps are not a proved
// exhaustive write-set. Each fallback candidate needs its own description.
func (a *HTTPAdapter) DescribeProspectiveEffects(tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan, transition plan.TransitionKind) (plan.ProspectiveEffectEnvelope, error) {
	envelope, err := plan.DescribeProspectiveEffects(resolved, transition)
	if err != nil {
		return envelope, err
	}
	if transition != plan.TransitionInstall {
		return envelope, nil
	}
	target, ok := a.prospectiveRawFileSlot(tool, mc, resolved)
	if !ok {
		return envelope, nil
	}
	for i := range envelope.Steps {
		if envelope.Steps[i].Phase == "primary_install" {
			envelope.Steps[i].Targets = []plan.ProspectiveTarget{target}
			break
		}
	}
	return envelope, nil
}

// prospectiveRawFileSlot returns a *tentative* target for one narrow case:
// raw HTTP payload + explicit absolute install directory + explicit binary.
// The directory must exist so parent symlinks can be resolved. No target is
// inferred from PATH, archives, installer payloads, scope defaults or an
// absent directory. A file slot cannot prove the identity of any other writes.
func (a *HTTPAdapter) prospectiveRawFileSlot(tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) (plan.ProspectiveTarget, bool) {
	if a == nil || a.allowInstallerArtifacts || a.allowDebArtifacts || tool == nil || mc == nil || mc.Kind != "http" || len(entrypoints(mc)) != 0 ||
		resolved == nil || resolved.Tool.Name != tool.Name || resolved.Candidate.Method != "http" || len(resolved.Artifacts) != 1 {
		return plan.ProspectiveTarget{}, false
	}
	concrete := resolved.Artifacts[0]
	if concrete.LocalPath != "" || artifact.ValidateURL(concrete.URL, []string{"http", "https"}) != nil || isArchive(fileExtension(concrete.URL)) {
		return plan.ProspectiveTarget{}, false
	}
	contract, ok := methodkind.Lookup("http")
	if !ok || contract.Artifact == nil || contract.Artifact.ValidateArtifactAllowing(concrete.URL, nil) != nil {
		return plan.ProspectiveTarget{}, false
	}
	root, _ := mc.Config["extract_to"].(string)
	binary, _ := mc.Config["binary"].(string)
	root = config.ExpandHomeDir(root)
	if root == "" || !filepath.IsAbs(root) || binary == "" || validateBinaryName(binary) != nil {
		return plan.ProspectiveTarget{}, false
	}
	parent, err := filepath.EvalSymlinks(root)
	if err != nil {
		return plan.ProspectiveTarget{}, false
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return plan.ProspectiveTarget{}, false
	}
	target := filepath.Join(root, binary)
	entry, err := os.Lstat(target)
	if err == nil {
		if !entry.Mode().IsRegular() { // Symlink, directory, etc.: unknown write identity.
			return plan.ProspectiveTarget{}, false
		}
	} else if !os.IsNotExist(err) {
		return plan.ProspectiveTarget{}, false
	}
	return plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: filepath.Join(parent, binary)}, true
}
