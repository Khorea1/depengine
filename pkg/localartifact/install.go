package localartifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	artifactcontract "github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/plan"
)

// Install materializes a previously resolved local artifact without network
// access. Raw artifacts are installed as a single file; archives are extracted
// into a directory. The destination is replaced transactionally using staging
// in the same parent directory so the final rename stays on one filesystem.
func Install(resolved Resolved, destination string) error {
	if destination == "" || !filepath.IsAbs(destination) {
		return fmt.Errorf("local artifact destination must be an absolute path")
	}
	if err := resolved.Artifact.Validate(); err != nil {
		return fmt.Errorf("local artifact: %w", err)
	}
	if resolved.Artifact.LocalPath == "" || resolved.Path == "" || resolved.Artifact.Checksum == "" {
		return fmt.Errorf("local artifact resolution is incomplete")
	}
	info, err := os.Lstat(resolved.Path)
	if err != nil {
		return fmt.Errorf("revalidate local artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("revalidate local artifact: source is not a regular non-symlink file")
	}
	actualChecksum, err := checksumFile(resolved.Path)
	if err != nil {
		return fmt.Errorf("revalidate local artifact checksum: %w", err)
	}
	if err := verifyChecksum(resolved.Artifact.Checksum, actualChecksum); err != nil {
		return fmt.Errorf("revalidate local artifact checksum: %w", err)
	}

	switch resolved.Artifact.Kind {
	case plan.ArtifactRaw:
		return installRaw(resolved.Path, destination)
	case plan.ArtifactArchive:
		return installArchive(resolved.Path, resolved.Artifact.LocalPath, destination)
	default:
		return fmt.Errorf("local artifact kind %q is not installable", resolved.Artifact.Kind)
	}
}

func installRaw(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat local artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("local artifact source is not a regular file")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	stage, err := os.CreateTemp(parent, ".depengine-local-*")
	if err != nil {
		return fmt.Errorf("create staging file: %w", err)
	}
	stageName := stage.Name()
	committed := false
	defer func() {
		_ = stage.Close()
		if !committed {
			_ = os.Remove(stageName)
		}
	}()

	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open local artifact: %w", err)
	}
	_, copyErr := io.Copy(stage, in)
	closeInErr := in.Close()
	if copyErr != nil {
		return fmt.Errorf("copy local artifact: %w", copyErr)
	}
	if closeInErr != nil {
		return fmt.Errorf("close local artifact: %w", closeInErr)
	}
	if err := stage.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve local artifact permissions: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync staged local artifact: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged local artifact: %w", err)
	}
	if err := replacePath(stageName, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

func installArchive(source, projectPath, destination string) error {
	ext := artifactcontract.Extension(projectPath, artifactcontract.SupportedArchiveExtensions)
	switch ext {
	case ".zip", ".tar", ".tar.gz", ".tgz":
	default:
		return fmt.Errorf("local archive format %q requires an extraction backend not available in the offline stdlib installer", ext)
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".depengine-local-*")
	if err != nil {
		return fmt.Errorf("create archive staging directory: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
		}
	}()

	switch ext {
	case ".zip":
		err = extractZip(source, stage)
	default:
		err = extractTar(source, stage, ext == ".tar.gz" || ext == ".tgz")
	}
	if err != nil {
		return err
	}
	if err := replacePath(stage, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

func replacePath(stage, destination string) error {
	_, statErr := os.Lstat(destination)
	hadDestination := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect destination: %w", statErr)
	}
	var backupDir, backup string
	if hadDestination {
		var err error
		backupDir, err = os.MkdirTemp(filepath.Dir(destination), ".depengine-backup-*")
		if err != nil {
			return fmt.Errorf("create destination backup directory: %w", err)
		}
		backup = filepath.Join(backupDir, "original")
		if err := os.Rename(destination, backup); err != nil {
			_ = os.Remove(backupDir)
			return fmt.Errorf("backup destination: %w", err)
		}
	}
	if err := os.Rename(stage, destination); err != nil {
		if hadDestination {
			_ = os.Rename(backup, destination)
			_ = os.Remove(backupDir)
		}
		return fmt.Errorf("commit local artifact: %w", err)
	}
	if hadDestination {
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("remove destination backup: %w", err)
		}
	}
	return nil
}

func extractZip(source, destination string) error {
	r, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open local zip: %w", err)
	}
	defer r.Close()
	for _, entry := range r.File {
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("local archive entry %q is a symlink", entry.Name)
		}
		target, err := safeArchiveTarget(destination, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, entry.Mode().Perm()); err != nil {
				return fmt.Errorf("create archive directory %q: %w", entry.Name, err)
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("local archive entry %q is not a regular file", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create archive parent %q: %w", entry.Name, err)
		}
		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open archive entry %q: %w", entry.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm())
		if err != nil {
			_ = rc.Close()
			return fmt.Errorf("create archive entry %q: %w", entry.Name, err)
		}
		_, copyErr := io.Copy(out, rc)
		closeOutErr := out.Close()
		closeInErr := rc.Close()
		if copyErr != nil {
			return fmt.Errorf("extract archive entry %q: %w", entry.Name, copyErr)
		}
		if closeOutErr != nil || closeInErr != nil {
			return fmt.Errorf("close archive entry %q", entry.Name)
		}
	}
	return nil
}

func extractTar(source, destination string, gzipped bool) error {
	f, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open local tar: %w", err)
	}
	defer f.Close()
	var reader io.Reader = f
	var gz *gzip.Reader
	if gzipped {
		gz, err = gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("open local gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read local tar: %w", err)
		}
		target, err := safeArchiveTarget(destination, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode).Perm()); err != nil {
				return fmt.Errorf("create archive directory %q: %w", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create archive parent %q: %w", hdr.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return fmt.Errorf("create archive entry %q: %w", hdr.Name, err)
			}
			_, copyErr := io.CopyN(out, tr, hdr.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("extract archive entry %q: %w", hdr.Name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close archive entry %q: %w", hdr.Name, closeErr)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("local archive entry %q is a link", hdr.Name)
		default:
			return fmt.Errorf("local archive entry %q has unsupported type %d", hdr.Name, hdr.Typeflag)
		}
	}
	return nil
}

func safeArchiveTarget(root, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("local archive contains invalid entry name")
	}
	cleanSlash := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(cleanSlash, "/") {
		return "", fmt.Errorf("local archive entry %q is absolute", name)
	}
	clean := filepath.Clean(filepath.FromSlash(cleanSlash))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("local archive entry %q escapes destination", name)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("local archive entry %q escapes destination", name)
	}
	return target, nil
}
