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

	artifactcontract "github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/plan"
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
	Mode     os.FileMode
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
	full, info, err := resolveRegularFileNoSymlinks(realRoot, portable)
	if err != nil {
		return Resolved{}, err
	}

	kind, err := classify(portable)
	if err != nil {
		return Resolved{}, err
	}

	checksum, err := checksumVerifiedRegularFile(full, info)
	if err != nil {
		return Resolved{}, fmt.Errorf("hash local artifact %q: %w", portable, err)
	}
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
		Mode: info.Mode().Perm(),
	}, nil
}

func resolveRegularFileNoSymlinks(root, portable string) (string, os.FileInfo, error) {
	current := root
	parts := strings.Split(filepath.FromSlash(portable), string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", nil, fmt.Errorf("stat local artifact %q: %w", portable, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("local artifact %q must not traverse symlinks", portable)
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return "", nil, fmt.Errorf("local artifact %q has non-directory path component %q", portable, part)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("local artifact %q is not a regular file", portable)
		}
		return current, info, nil
	}
	return "", nil, fmt.Errorf("local artifact path is required")
}

var supportedOfflineArchiveExtensions = []string{".tar.gz", ".tgz", ".tar", ".zip"}

// ClassifyProjectPath validates a portable project-relative path and returns
// the materialization kind supported by the offline installer. It does not
// access the filesystem.
func ClassifyProjectPath(projectPath string) (plan.ArtifactKind, error) {
	portable, err := plan.NormalizeProjectPath(projectPath)
	if err != nil {
		return "", err
	}
	kind, err := classify(portable)
	if err != nil {
		return "", err
	}
	if kind == KindArchive {
		return plan.ArtifactArchive, nil
	}
	return plan.ArtifactRaw, nil
}

func classify(name string) (Kind, error) {
	if ext := artifactcontract.InstallerExtension(name); ext != "" {
		return "", &artifactcontract.ForbiddenExtensionError{Extension: ext}
	}
	if ext := artifactcontract.Extension(name, supportedOfflineArchiveExtensions); ext != "" {
		return KindArchive, nil
	}
	if ext := artifactcontract.Extension(name, artifactcontract.SupportedArchiveExtensions); ext != "" {
		return "", &artifactcontract.UnsupportedArchiveError{Extension: ext}
	}
	if ext := artifactcontract.Extension(name, artifactcontract.UnsupportedArchiveExtensions); ext != "" {
		return "", &artifactcontract.UnsupportedArchiveError{Extension: ext}
	}
	return KindRaw, nil
}

func openVerifiedRegularFile(path string, expected os.FileInfo) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path) // #nosec G304 -- Local artifacts are explicitly selected by the operator and revalidated after open.
	if err != nil {
		return nil, nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !opened.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("opened path is not a regular file")
	}
	if expected != nil && !os.SameFile(expected, opened) {
		_ = f.Close()
		return nil, nil, fmt.Errorf("local artifact changed between validation and open")
	}
	return f, opened, nil
}

func checksumOpenFile(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func checksumVerifiedRegularFile(path string, expected os.FileInfo) (string, error) {
	f, _, err := openVerifiedRegularFile(path, expected)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return checksumOpenFile(f)
}

// VerifyRegularFileChecksum verifies a regular, non-symlink file against an
// expected sha256:<hex> digest. It is intended for post-install drift checks.
func VerifyRegularFileChecksum(path, expectedChecksum string) error {
	return VerifyRegularFileState(path, expectedChecksum, 0)
}

// VerifyRegularFileState verifies both content identity and, when non-zero,
// the expected permission bits of an installed raw artifact. File type and
// symlink checks are always enforced.
func VerifyRegularFileState(path, expectedChecksum string, expectedMode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular non-symlink file")
	}
	if expectedMode != 0 && info.Mode().Perm() != expectedMode.Perm() {
		return fmt.Errorf("file permissions drifted: got %o want %o", info.Mode().Perm(), expectedMode.Perm())
	}
	actual, err := checksumVerifiedRegularFile(path, info)
	if err != nil {
		return err
	}
	return verifyChecksum(expectedChecksum, actual)
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
