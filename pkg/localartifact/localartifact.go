// Package localartifact resolves vendored/offline artifact files without any
// network access. Project-relative identity is kept separate from the absolute
// filesystem path used only while executing on the current host.
package localartifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	artifactcontract "github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/plan"
)

// Kind is the local payload classification used by an installer after
// resolution. Platform installer formats are deliberately not accepted here;
// they require dedicated typed adapters.
type Kind string

const (
	KindRaw     Kind = "raw"
	KindArchive Kind = "archive"
)

// Resolved is a verified local artifact. Path is execution-local and must not
// be persisted to a lockfile; Artifact.LocalPath is the portable project-
// relative identity that may be persisted.
type Resolved struct {
	Artifact plan.Artifact
	Path     string
	Size     int64
}

// Resolve validates a project-relative local artifact, opens exactly that
// regular file without following a final symlink, computes sha256, optionally
// verifies the expected checksum, and classifies its payload shape. It performs
// no network operations and no host mutations.
func Resolve(projectRoot, projectPath, expectedChecksum string) (Resolved, error) {
	if projectRoot == "" || !filepath.IsAbs(projectRoot) {
		return Resolved{}, fmt.Errorf("project root must be an absolute path")
	}
	portable, err := plan.NormalizeProjectPath(projectPath)
	if err != nil {
		return Resolved{}, err
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve project root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve project root symlinks: %w", err)
	}
	full := filepath.Join(realRoot, filepath.FromSlash(portable))
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve local artifact %q: %w", portable, err)
	}
	rel, err := filepath.Rel(realRoot, realFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Resolved{}, fmt.Errorf("local artifact path escapes the project root")
	}
	full = realFull

	info, err := os.Lstat(full)
	if err != nil {
		return Resolved{}, fmt.Errorf("stat local artifact %q: %w", portable, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Resolved{}, fmt.Errorf("local artifact %q must not be a symlink", portable)
	}
	if !info.Mode().IsRegular() {
		return Resolved{}, fmt.Errorf("local artifact %q is not a regular file", portable)
	}

	kind, err := classify(portable)
	if err != nil {
		return Resolved{}, err
	}

	f, err := os.Open(full)
	if err != nil {
		return Resolved{}, fmt.Errorf("open local artifact %q: %w", portable, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Resolved{}, fmt.Errorf("hash local artifact %q: %w", portable, err)
	}
	checksum := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if expectedChecksum != "" {
		if err := verifyChecksum(expectedChecksum, checksum); err != nil {
			return Resolved{}, fmt.Errorf("local artifact %q: %w", portable, err)
		}
	}

	artifactKind := plan.ArtifactRaw
	if kind == KindArchive {
		artifactKind = plan.ArtifactArchive
	}
	return Resolved{
		Artifact: plan.Artifact{
			Kind:      artifactKind,
			LocalPath: portable,
			Checksum:  checksum,
		},
		Path: full,
		Size: info.Size(),
	}, nil
}

func classify(name string) (Kind, error) {
	if ext := artifactcontract.InstallerExtension(name); ext != "" {
		return "", &artifactcontract.ForbiddenExtensionError{Extension: ext}
	}
	if ext := artifactcontract.Extension(name, artifactcontract.SupportedArchiveExtensions); ext != "" {
		return KindArchive, nil
	}
	if ext := artifactcontract.Extension(name, artifactcontract.UnsupportedArchiveExtensions); ext != "" {
		return "", &artifactcontract.UnsupportedArchiveError{Extension: ext}
	}
	return KindRaw, nil
}

func checksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func verifyChecksum(expected, actual string) error {
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "sha256") {
		return fmt.Errorf("expected checksum must use sha256:<hex>")
	}
	hexValue := strings.ToLower(parts[1])
	if len(hexValue) != sha256.Size*2 {
		return fmt.Errorf("invalid sha256 checksum length")
	}
	if _, err := hex.DecodeString(hexValue); err != nil {
		return fmt.Errorf("invalid sha256 checksum: %w", err)
	}
	if !strings.EqualFold("sha256:"+hexValue, actual) {
		return errors.New("checksum mismatch")
	}
	return nil
}
