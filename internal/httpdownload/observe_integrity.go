package httpdownload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
)

// verifyInstalledRawChecksum checks the actual installed bytes only when the
// transport is known to copy a raw payload into a known file destination and
// the recipe provides a literal checksum. Archive checksums refer to archives,
// not extracted files, and cannot establish installed-byte identity.
//
// This is an additional safety check, not a general proof of ownership. In
// particular, PATH-only installs and auto/remote checksum sources cannot be
// verified by this function. A pinned raw payload whose physical destination
// is unknown is reported as inconclusive, not incorrectly accepted as already.
func (a *HTTPAdapter) verifyInstalledRawChecksum(tool *config.Tool, mc *config.MethodCandidate) (required, verified bool, err error) {
	checksum, _ := mc.Config["checksum"].(string)
	if checksum == "" || strings.HasSuffix(checksum, ":auto") || a.allowInstallerArtifacts || a.allowDebArtifacts || len(entrypoints(mc)) > 0 {
		return false, false, nil
	}
	// A GitHub candidate may specify an asset instead of a URL. If the format
	// is not resolved, do not mistake an archive checksum for an executable.
	artifactName, _ := mc.Config["url"].(string)
	if artifactName == "" {
		assetName, _ := mc.Config["asset"].(string)
		artifactName = assetName
	}
	if artifactName == "" {
		// The format is not known; accepting it as a proven raw payload
		// would be unsafe, while attempting to hash an archive is wrong.
		return true, false, nil
	}
	if isArchive(fileExtension(artifactName)) || fileExtension(artifactName) == ".deb" {
		return false, false, nil
	}
	root, _ := mc.Config["extract_to"].(string)
	if root == "" && scopeConfigured(mc) {
		root = PlacementOrDefault(tool, mc, "", "").InstallRoot
	}
	if root == "" {
		// A PATH lookup does not establish where the actual payload lives.
		return true, false, nil
	}
	name, _ := mc.Config["binary"].(string)
	if name == "" && tool != nil {
		name = tool.Name
	}
	if err := validateBinaryName(name); err != nil {
		return true, false, fmt.Errorf("invalid installed binary name: %w", err)
	}
	if err := VerifyChecksum(filepath.Join(config.ExpandHomeDir(root), name), checksum); err != nil {
		return true, false, err
	}
	return true, true, nil
}

// observeExplicitHTTPFile checks a known installation destination without
// conflating an inaccessible/occupied path with a genuinely missing file.
// It deliberately does not cover archive entrypoints or PATH-only probes:
// those have distinct observation contracts.
func observeExplicitHTTPFile(tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, bool) {
	if mc == nil || len(entrypoints(mc)) > 0 {
		return plan.Observation{}, false
	}
	root, _ := mc.Config["extract_to"].(string)
	if root == "" && scopeConfigured(mc) {
		root = PlacementOrDefault(tool, mc, "", "").InstallRoot
	}
	if root == "" {
		return plan.Observation{}, false
	}
	name, _ := mc.Config["binary"].(string)
	if name == "" && tool != nil {
		name = tool.Name
	}
	if err := validateBinaryName(name); err != nil {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: "invalid installed binary name: " + err.Error()}, true
	}
	target := filepath.Join(config.ExpandHomeDir(root), name)
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return observeMissingHTTPFileParent(config.ExpandHomeDir(root)), true
		}

		return plan.Observation{Presence: plan.PresenceUnknown, Detail: fmt.Sprintf("cannot inspect installed file %q: %v", target, err)}, true
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// A dangling/cyclic link occupies the destination even though Stat
		// cannot follow it. It must never become evidence of absence.
		info, err = os.Stat(target)
		if err != nil {
			return plan.Observation{Presence: plan.PresenceBroken, Detail: fmt.Sprintf("installed file symlink %q cannot be resolved: %v", target, err)}, true
		}
	}
	if !info.Mode().IsRegular() {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: "installed binary destination is occupied by a non-regular file"}, true
	}
	return plan.Observation{Presence: plan.PresencePresent}, true
}

// observeMissingHTTPFileParent walks existing path ancestors when the target
// itself is missing. A missing suffix is safe only when an existing directory
// anchors it. In particular, a dangling symlink or a non-directory ancestor
// is not evidence that the destination is available for installation.
func observeMissingHTTPFileParent(root string) plan.Observation {
	for dir := filepath.Clean(root); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(dir)
				if err != nil {
					return plan.Observation{Presence: plan.PresenceUnknown, Detail: fmt.Sprintf("cannot inspect installed file parent: %v", err)}
				}
			}
			if !info.IsDir() {
				return plan.Observation{Presence: plan.PresenceUnknown, Detail: "installed file parent is not a directory"}
			}
			return plan.Observation{Presence: plan.PresenceAbsent}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: fmt.Sprintf("cannot inspect installed file parent: %v", err)}
		}
		if parent := filepath.Dir(dir); parent == dir {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: "cannot establish an existing installation parent"}
		}
	}
}

// observedRawFileSlot reports a canonical physical file *slot* only for a
// directly installed raw file at an explicit destination. This identifies
// where an install can act, not who owns the file or whether it was installed
// by depengine. It intentionally emits no evidence for archives, installers,
// PATH-only tools, unknown release assets or symbolic-link destinations.
//
// Parent symlinks are resolved so two schemas naming the same slot through
// different directory aliases do not appear independent. This probe is
// read-only and racy by nature: claims need a host-wide transaction and a
// second observation before any mutation.
func (a *HTTPAdapter) observedRawFileSlot(tool *config.Tool, mc *config.MethodCandidate) (plan.ObservedResource, bool) {
	if a.allowInstallerArtifacts || a.allowDebArtifacts || mc == nil || len(entrypoints(mc)) != 0 {
		return plan.ObservedResource{}, false
	}
	artifactName, _ := mc.Config["url"].(string)
	if artifactName == "" {
		artifactName, _ = mc.Config["asset"].(string)
	}
	if artifactName == "" || isArchive(fileExtension(artifactName)) || fileExtension(artifactName) == ".deb" {
		return plan.ObservedResource{}, false
	}
	contract, ok := methodkind.Lookup("http")
	if !ok || contract.Artifact == nil {
		return plan.ObservedResource{}, false
	}
	var allowed []string
	if portableExecutableArtifactAllowed(mc, artifactName) {
		allowed = []string{".exe"}
	}
	if contract.Artifact.ValidateArtifactAllowing(artifactName, allowed) != nil {
		// A declared platform installer is not a raw file, even when a file
		// with the same name exists at the configured binary destination.
		return plan.ObservedResource{}, false
	}
	root, _ := mc.Config["extract_to"].(string)
	if root == "" && scopeConfigured(mc) {
		root = PlacementOrDefault(tool, mc, "", "").InstallRoot
	}
	if root == "" {
		return plan.ObservedResource{}, false
	}
	name, _ := mc.Config["binary"].(string)
	if name == "" && tool != nil {
		name = tool.Name
	}
	if name == "" || validateBinaryName(name) != nil {
		return plan.ObservedResource{}, false
	}
	absolute, err := filepath.Abs(filepath.Join(config.ExpandHomeDir(root), name))
	if err != nil {
		return plan.ObservedResource{}, false
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		// This also excludes symlink destinations: the link's directory
		// entry and its target are different mutation resources.
		return plan.ObservedResource{}, false
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return plan.ObservedResource{}, false
	}
	return plan.ObservedResource{
		Scope:    "host-filesystem",
		Kind:     "file-slot",
		Identity: filepath.Join(parent, filepath.Base(absolute)),
	}, true
}
