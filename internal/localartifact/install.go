package localartifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	artifactcontract "github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/plan"
)

const (
	archiveChecksumMarker = ".depengine-local-artifact.sha256"
	archiveTreeMarker     = ".depengine-local-artifact-tree.sha256"
	// archiveExpansionLimit caps the aggregate uncompressed regular-file data
	// written while extracting one ZIP or TAR archive.
	archiveExpansionLimit int64 = 4 << 30
)

type archiveExpansionBudget struct {
	used  int64
	limit int64
}

func newArchiveExpansionBudget() *archiveExpansionBudget {
	return &archiveExpansionBudget{limit: archiveExpansionLimit}
}

func (b *archiveExpansionBudget) writer(dst io.Writer) io.Writer {
	return archiveBudgetWriter{dst: dst, budget: b}
}

type archiveBudgetWriter struct {
	dst    io.Writer
	budget *archiveExpansionBudget
}

func (w archiveBudgetWriter) Write(p []byte) (int, error) {
	remaining := w.budget.limit - w.budget.used
	if remaining <= 0 {
		return 0, fmt.Errorf("archive regular-file expansion exceeds %d-byte limit", w.budget.limit)
	}
	if int64(len(p)) > remaining {
		n, err := w.dst.Write(p[:int(remaining)])
		w.budget.used += int64(n)
		if err != nil {
			return n, err
		}
		return n, fmt.Errorf("archive regular-file expansion exceeds %d-byte limit", w.budget.limit)
	}
	n, err := w.dst.Write(p)
	w.budget.used += int64(n)
	return n, err
}

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
	validatedInfo, err := revalidateResolvedSource(resolved)
	if err != nil {
		return fmt.Errorf("revalidate local artifact: %w", err)
	}
	source, sourceInfo, err := openVerifiedRegularFile(resolved.Path, validatedInfo)
	if err != nil {
		return fmt.Errorf("revalidate local artifact open: %w", err)
	}
	defer source.Close()
	actualChecksum, err := checksumOpenFile(source)
	if err != nil {
		return fmt.Errorf("revalidate local artifact checksum: %w", err)
	}
	if err := verifyChecksum(resolved.Artifact.Checksum, actualChecksum); err != nil {
		return fmt.Errorf("revalidate local artifact checksum: %w", err)
	}

	switch resolved.Artifact.Kind {
	case plan.ArtifactRaw:
		if sourceInfo.Mode().Perm() != resolved.Mode.Perm() {
			return fmt.Errorf("revalidate local artifact permissions: source mode changed from %o to %o", resolved.Mode.Perm(), sourceInfo.Mode().Perm())
		}
		if err := VerifyRegularFileState(destination, resolved.Artifact.Checksum, resolved.Mode); err == nil {
			return nil
		}
		return installRaw(source, sourceInfo, resolved.Artifact.Checksum, destination)
	case plan.ArtifactArchive:
		if err := VerifyArchiveChecksum(destination, resolved.Artifact.Checksum); err == nil {
			return nil
		}
		snapshot, snapshotSize, cleanup, err := snapshotVerifiedSource(source, resolved.Artifact.Checksum, filepath.Dir(destination))
		if err != nil {
			return fmt.Errorf("snapshot local archive: %w", err)
		}
		defer cleanup()
		return installArchive(snapshot, snapshotSize, resolved.Artifact.LocalPath, resolved.Artifact.Checksum, destination)
	default:
		return fmt.Errorf("local artifact kind %q is not installable", resolved.Artifact.Kind)
	}
}

func revalidateResolvedSource(resolved Resolved) (os.FileInfo, error) {
	portable, err := plan.NormalizeProjectPath(resolved.Artifact.LocalPath)
	if err != nil {
		return nil, err
	}
	if resolved.Path == "" || !filepath.IsAbs(resolved.Path) {
		return nil, fmt.Errorf("resolved source path must be absolute")
	}
	root := filepath.Clean(resolved.Path)
	parts := strings.Split(filepath.FromSlash(portable), string(filepath.Separator))
	for range parts {
		root = filepath.Dir(root)
	}
	full, info, err := resolveRegularFileNoSymlinks(root, portable)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(full) != filepath.Clean(resolved.Path) {
		return nil, fmt.Errorf("resolved source path does not match project-relative identity")
	}
	return info, nil
}

func installRaw(source *os.File, info os.FileInfo, expectedChecksum, destination string) error {
	if source == nil || info == nil || !info.Mode().IsRegular() {
		return fmt.Errorf("local artifact source is not a verified regular file")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind local artifact: %w", err)
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

	if _, err := io.Copy(stage, source); err != nil {
		return fmt.Errorf("copy local artifact: %w", err)
	}
	if err := stage.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve local artifact permissions: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync staged local artifact: %w", err)
	}
	stagedChecksum, err := checksumOpenFile(stage)
	if err != nil {
		return fmt.Errorf("checksum staged local artifact: %w", err)
	}
	if err := verifyChecksum(expectedChecksum, stagedChecksum); err != nil {
		return fmt.Errorf("staged local artifact changed during materialization: %w", err)
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

func snapshotVerifiedSource(source *os.File, expectedChecksum, parent string) (*os.File, int64, func(), error) {
	if source == nil {
		return nil, 0, func() {}, errors.New("verified source is required")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, 0, func() {}, fmt.Errorf("create snapshot parent: %w", err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, 0, func() {}, fmt.Errorf("rewind verified source: %w", err)
	}
	snapshot, err := os.CreateTemp(parent, ".depengine-source-*")
	if err != nil {
		return nil, 0, func() {}, fmt.Errorf("create source snapshot: %w", err)
	}
	name := snapshot.Name()
	cleanup := func() {
		_ = snapshot.Close()
		_ = os.Remove(name)
	}
	if _, err := io.Copy(snapshot, source); err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("copy source snapshot: %w", err)
	}
	if err := snapshot.Sync(); err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("sync source snapshot: %w", err)
	}
	checksum, err := checksumOpenFile(snapshot)
	if err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("checksum source snapshot: %w", err)
	}
	if err := verifyChecksum(expectedChecksum, checksum); err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("source changed during snapshot: %w", err)
	}
	info, err := snapshot.Stat()
	if err != nil {
		cleanup()
		return nil, 0, func() {}, fmt.Errorf("stat source snapshot: %w", err)
	}
	return snapshot, info.Size(), cleanup, nil
}

func installArchive(source *os.File, sourceSize int64, projectPath, checksum, destination string) error {
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
	// MkdirTemp intentionally creates 0700 directories. That is suitable for
	// temporary work but would otherwise become the installed payload root after
	// the atomic rename. Normalize the default installed root mode; an explicit
	// archive root directory entry may still override it during extraction.
	if err := os.Chmod(stage, 0o755); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("set archive staging root permissions: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
		}
	}()

	var directoryModes map[string]os.FileMode
	switch ext {
	case ".zip":
		directoryModes, err = extractZip(source, sourceSize, stage)
	default:
		directoryModes, err = extractTar(source, stage, ext == ".tar.gz" || ext == ".tgz")
	}
	if err != nil {
		return err
	}
	for _, reserved := range []string{archiveChecksumMarker, archiveTreeMarker} {
		marker := filepath.Join(stage, reserved)
		if _, err := os.Lstat(marker); err == nil {
			return fmt.Errorf("local archive contains reserved metadata entry %q", reserved)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect local archive metadata marker %q: %w", reserved, err)
		}
	}
	if err := os.WriteFile(filepath.Join(stage, archiveChecksumMarker), []byte(checksum+"\n"), 0o644); err != nil {
		return fmt.Errorf("write local archive metadata marker: %w", err)
	}
	treeMarkerPath := filepath.Join(stage, archiveTreeMarker)
	treeMarker, err := os.OpenFile(treeMarkerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create local archive tree marker: %w", err)
	}
	markerClosed := false
	defer func() {
		if !markerClosed {
			_ = treeMarker.Close()
		}
	}()
	if err := applyArchiveDirectoryModes(directoryModes); err != nil {
		return err
	}
	treeChecksum, err := checksumDirectoryPayload(stage)
	if err != nil {
		return fmt.Errorf("checksum extracted local archive: %w", err)
	}
	if _, err := io.WriteString(treeMarker, treeChecksum+"\n"); err != nil {
		return fmt.Errorf("write local archive tree marker: %w", err)
	}
	if err := treeMarker.Sync(); err != nil {
		return fmt.Errorf("sync local archive tree marker: %w", err)
	}
	if err := treeMarker.Close(); err != nil {
		return fmt.Errorf("close local archive tree marker: %w", err)
	}
	markerClosed = true
	if err := replacePath(stage, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

// VerifyArchiveChecksum verifies that an installed archive directory was
// materialized from the expected vendored content. The marker is written into
// the transaction staging directory, so its presence and checksum commit
// atomically with the extracted payload.
func VerifyArchiveChecksum(destination, expectedChecksum string) error {
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("path is not a non-symlink directory")
	}
	marker := filepath.Join(destination, archiveChecksumMarker)
	markerInfo, err := os.Lstat(marker)
	if err != nil {
		return err
	}
	if markerInfo.Mode()&os.ModeSymlink != 0 || !markerInfo.Mode().IsRegular() {
		return fmt.Errorf("local archive metadata marker is not a regular non-symlink file")
	}
	markerFile, _, err := openVerifiedRegularFile(marker, markerInfo)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(markerFile)
	closeErr := markerFile.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	actual := strings.TrimSpace(string(data))
	if actual == "" {
		return fmt.Errorf("local archive metadata marker is empty")
	}
	if err := verifyChecksum(expectedChecksum, actual); err != nil {
		return err
	}

	treeMarker := filepath.Join(destination, archiveTreeMarker)
	treeInfo, err := os.Lstat(treeMarker)
	if err != nil {
		return err
	}
	if treeInfo.Mode()&os.ModeSymlink != 0 || !treeInfo.Mode().IsRegular() {
		return fmt.Errorf("local archive tree marker is not a regular non-symlink file")
	}
	treeFile, _, err := openVerifiedRegularFile(treeMarker, treeInfo)
	if err != nil {
		return err
	}
	treeData, readErr := io.ReadAll(treeFile)
	closeErr = treeFile.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	expectedTree := strings.TrimSpace(string(treeData))
	if expectedTree == "" {
		return fmt.Errorf("local archive tree marker is empty")
	}
	actualTree, err := checksumDirectoryPayload(destination)
	if err != nil {
		return err
	}
	return verifyChecksum(expectedTree, actualTree)
}

func checksumDirectoryPayload(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			current, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(info, current) {
				return errors.New("installed archive root changed during verification")
			}
			_, _ = fmt.Fprintf(h, "R\x00%o\x00", info.Mode().Perm())
			return nil
		}
		if rel == archiveChecksumMarker || rel == archiveTreeMarker {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("installed archive entry %q is a symlink", filepath.ToSlash(rel))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case info.IsDir():
			current, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || !os.SameFile(info, current) {
				return fmt.Errorf("installed archive directory %q changed during verification", rel)
			}
			_, _ = fmt.Fprintf(h, "D\x00%s\x00%o\x00", rel, info.Mode().Perm())
			return nil
		case info.Mode().IsRegular():
			_, _ = fmt.Fprintf(h, "F\x00%s\x00%o\x00%d\x00", rel, info.Mode().Perm(), info.Size())
			f, _, err := openVerifiedRegularFile(path, info)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(h, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			_, _ = h.Write([]byte{0})
			return nil
		default:
			return fmt.Errorf("installed archive entry %q has unsupported type %v", rel, info.Mode().Type())
		}
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

type replacePathOps struct {
	lstat     func(string) (os.FileInfo, error)
	mkdirTemp func(string, string) (string, error)
	rename    func(string, string) error
	remove    func(string) error
	removeAll func(string) error
}

var defaultReplacePathOps = replacePathOps{
	lstat:     os.Lstat,
	mkdirTemp: os.MkdirTemp,
	rename:    os.Rename,
	remove:    os.Remove,
	removeAll: os.RemoveAll,
}

func replacePath(stage, destination string) error {
	return replacePathWithOps(stage, destination, defaultReplacePathOps)
}

func replacePathWithOps(stage, destination string, ops replacePathOps) error {
	_, statErr := ops.lstat(destination)
	hadDestination := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect destination: %w", statErr)
	}
	var backupDir, backup string
	if hadDestination {
		var err error
		backupDir, err = ops.mkdirTemp(filepath.Dir(destination), ".depengine-backup-*")
		if err != nil {
			return fmt.Errorf("create destination backup directory: %w", err)
		}
		backup = filepath.Join(backupDir, "original")
		if err := ops.rename(destination, backup); err != nil {
			_ = ops.remove(backupDir)
			return fmt.Errorf("backup destination: %w", err)
		}
	}
	if err := ops.rename(stage, destination); err != nil {
		commitErr := fmt.Errorf("commit local artifact: %w", err)
		if hadDestination {
			restoreErr := ops.rename(backup, destination)
			cleanupErr := ops.remove(backupDir)
			if restoreErr != nil {
				return errors.Join(commitErr, fmt.Errorf("restore previous destination: %w", restoreErr))
			}
			if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				return errors.Join(commitErr, fmt.Errorf("remove destination backup directory: %w", cleanupErr))
			}
		}
		return commitErr
	}
	if hadDestination {
		if cleanupErr := ops.removeAll(backupDir); cleanupErr != nil {
			// A failed cleanup after the commit must not be reported as an ordinary
			// candidate failure while leaving the new destination installed. A
			// caller may legitimately fall back to another candidate on error. Move
			// the newly committed payload back to the staging path and restore the
			// previous destination so the failed candidate is observationally
			// rolled back whenever the filesystem permits it.
			rollbackNewErr := ops.rename(destination, stage)
			if rollbackNewErr != nil {
				return errors.Join(
					fmt.Errorf("cleanup destination backup after commit: %w", cleanupErr),
					fmt.Errorf("rollback committed local artifact: %w", rollbackNewErr),
				)
			}
			restoreErr := ops.rename(backup, destination)
			if restoreErr != nil {
				// The previous destination could not be restored. Do not leave the
				// destination absent while the newly installed payload sits in staging
				// (and may be removed by the caller's deferred cleanup). Best-effort
				// recommit the new payload so at least one complete destination remains.
				recommitErr := ops.rename(stage, destination)
				if recommitErr != nil {
					return errors.Join(
						fmt.Errorf("cleanup destination backup after commit: %w", cleanupErr),
						fmt.Errorf("restore previous destination after cleanup failure: %w", restoreErr),
						fmt.Errorf("recommit new destination after restore failure: %w", recommitErr),
					)
				}
				return errors.Join(
					fmt.Errorf("cleanup destination backup after commit: %w", cleanupErr),
					fmt.Errorf("restore previous destination after cleanup failure: %w", restoreErr),
				)
			}
			if err := ops.remove(backupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.Join(
					fmt.Errorf("cleanup destination backup after commit: %w", cleanupErr),
					fmt.Errorf("remove restored destination backup directory: %w", err),
				)
			}
			return fmt.Errorf("cleanup destination backup after commit: %w", cleanupErr)
		}
	}
	return nil
}

func extractZip(source *os.File, sourceSize int64, destination string) (map[string]os.FileMode, error) {
	if source == nil {
		return nil, fmt.Errorf("open local zip: verified source is required")
	}
	r, err := zip.NewReader(source, sourceSize)
	if err != nil {
		return nil, fmt.Errorf("open local zip: %w", err)
	}
	entries := newArchiveEntryRegistry()
	directoryModes := make(map[string]os.FileMode)
	budget := newArchiveExpansionBudget()
	for _, entry := range r.File {
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("local archive entry %q is a symlink", entry.Name)
		}
		target, err := safeArchiveTarget(destination, entry.Name)
		if err != nil {
			return nil, err
		}
		if err := entries.add(entry.Name, entry.FileInfo().IsDir()); err != nil {
			return nil, err
		}
		if entry.FileInfo().IsDir() {
			mode, err := validatedArchiveDirectoryMode(entry.Mode().Perm())
			if err != nil {
				return nil, fmt.Errorf("archive directory %q: %w", entry.Name, err)
			}
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return nil, fmt.Errorf("create archive directory %q: %w", entry.Name, err)
			}
			directoryModes[target] = mode
			continue
		}
		if !entry.Mode().IsRegular() {
			return nil, fmt.Errorf("local archive entry %q is not a regular file", entry.Name)
		}
		fileMode, err := validatedArchiveFileMode(entry.Mode().Perm())
		if err != nil {
			return nil, fmt.Errorf("archive file %q: %w", entry.Name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, fmt.Errorf("create archive parent %q: %w", entry.Name, err)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open archive entry %q: %w", entry.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("create archive entry %q: %w", entry.Name, err)
		}
		_, copyErr := io.Copy(budget.writer(out), rc) // #nosec G110 -- The writer caps aggregate expanded bytes at archiveExpansionLimit.
		closeOutErr := out.Close()
		closeInErr := rc.Close()
		if copyErr != nil || closeOutErr != nil || closeInErr != nil {
			return nil, errors.Join(
				wrapArchiveEntryError("extract", entry.Name, copyErr),
				wrapArchiveEntryError("close output for", entry.Name, closeOutErr),
				wrapArchiveEntryError("close input for", entry.Name, closeInErr),
			)
		}
	}
	return directoryModes, nil
}

func extractTar(source *os.File, destination string, gzipped bool) (map[string]os.FileMode, error) {
	if source == nil {
		return nil, fmt.Errorf("open local tar: verified source is required")
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind local tar: %w", err)
	}
	var reader io.Reader = source
	var gz *gzip.Reader
	var err error
	if gzipped {
		gz, err = gzip.NewReader(source)
		if err != nil {
			return nil, fmt.Errorf("open local gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	tr := tar.NewReader(reader)
	entries := newArchiveEntryRegistry()
	directoryModes := make(map[string]os.FileMode)
	budget := newArchiveExpansionBudget()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read local tar: %w", err)
		}
		target, err := safeArchiveTarget(destination, hdr.Name)
		if err != nil {
			return nil, err
		}
		isDir := hdr.Typeflag == tar.TypeDir
		isFile := hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA
		if isFile && hdr.Size < 0 {
			return nil, fmt.Errorf("local archive entry %q has negative size", hdr.Name)
		}
		if isDir || isFile {
			if err := entries.add(hdr.Name, isDir); err != nil {
				return nil, err
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			archiveMode, err := tarPermissionMode(hdr.Mode)
			if err != nil {
				return nil, fmt.Errorf("archive directory %q: %w", hdr.Name, err)
			}
			mode, err := validatedArchiveDirectoryMode(archiveMode)
			if err != nil {
				return nil, fmt.Errorf("archive directory %q: %w", hdr.Name, err)
			}
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return nil, fmt.Errorf("create archive directory %q: %w", hdr.Name, err)
			}
			directoryModes[target] = mode
		case tar.TypeReg, tar.TypeRegA:
			archiveMode, err := tarPermissionMode(hdr.Mode)
			if err != nil {
				return nil, fmt.Errorf("archive file %q: %w", hdr.Name, err)
			}
			fileMode, err := validatedArchiveFileMode(archiveMode)
			if err != nil {
				return nil, fmt.Errorf("archive file %q: %w", hdr.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, fmt.Errorf("create archive parent %q: %w", hdr.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
			if err != nil {
				return nil, fmt.Errorf("create archive entry %q: %w", hdr.Name, err)
			}
			_, copyErr := io.CopyN(budget.writer(out), tr, hdr.Size)
			closeErr := out.Close()
			if copyErr != nil || closeErr != nil {
				return nil, errors.Join(
					wrapArchiveEntryError("extract", hdr.Name, copyErr),
					wrapArchiveEntryError("close", hdr.Name, closeErr),
				)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return nil, fmt.Errorf("local archive entry %q is a link", hdr.Name)
		default:
			return nil, fmt.Errorf("local archive entry %q has unsupported type %d", hdr.Name, hdr.Typeflag)
		}
	}
	return directoryModes, nil
}

// tarPermissionMode validates the signed archive mode before translating its
// permission bits. TAR modes may include setuid, setgid, and sticky bits, but
// must not contain negative or out-of-range values that could be truncated.
func tarPermissionMode(mode int64) (os.FileMode, error) {
	if mode < 0 || mode > 0o7777 {
		return 0, fmt.Errorf("mode %d is outside the supported permission range", mode)
	}
	// The range check makes this conversion safe; the mask preserves the
	// permission-only behavior of os.FileMode.Perm().
	return os.FileMode(mode & 0o777), nil
}

func wrapArchiveEntryError(action, name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s archive entry %q: %w", action, name, err)
}

func validatedArchiveFileMode(mode os.FileMode) (os.FileMode, error) {
	mode = mode.Perm()
	if mode == 0 {
		mode = 0o644
	}
	if mode&0o400 == 0 {
		return 0, fmt.Errorf("final mode %o must keep owner read permission for verification", mode)
	}
	return mode, nil
}

func validatedArchiveDirectoryMode(mode os.FileMode) (os.FileMode, error) {
	mode = mode.Perm()
	if mode == 0 {
		mode = 0o755
	}
	if mode&0o500 != 0o500 {
		return 0, fmt.Errorf("final mode %o must keep owner read+execute permissions for verification", mode)
	}
	return mode, nil
}

func applyArchiveDirectoryModes(modes map[string]os.FileMode) error {
	paths := make([]string, 0, len(modes))
	for path := range modes {
		paths = append(paths, path)
	}
	slices.SortFunc(paths, func(a, b string) int {
		depthA := strings.Count(filepath.Clean(a), string(filepath.Separator))
		depthB := strings.Count(filepath.Clean(b), string(filepath.Separator))
		if depthA != depthB {
			return depthB - depthA
		}
		return strings.Compare(a, b)
	})
	for _, path := range paths {
		if err := os.Chmod(path, modes[path]); err != nil {
			return fmt.Errorf("set final archive directory permissions %q: %w", path, err)
		}
	}
	return nil
}

func safeArchiveTarget(root, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("local archive contains invalid entry name")
	}
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("local archive entry %q must use portable '/' separators", name)
	}
	cleanSlash := name
	if strings.HasPrefix(cleanSlash, "/") {
		return "", fmt.Errorf("local archive entry %q is absolute", name)
	}
	if hasWindowsArchiveVolumePrefix(cleanSlash) {
		return "", fmt.Errorf("local archive entry %q is drive-qualified", name)
	}
	if err := validatePortableArchiveName(cleanSlash); err != nil {
		return "", fmt.Errorf("local archive entry %q: %w", name, err)
	}
	cleanPortable := path.Clean(cleanSlash)
	if isReservedArchiveMetadataName(cleanPortable) {
		return "", fmt.Errorf("local archive contains reserved metadata entry %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(cleanPortable))
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

func isReservedArchiveMetadataName(name string) bool {
	if strings.Contains(name, "/") {
		return false
	}
	return strings.EqualFold(name, archiveChecksumMarker) || strings.EqualFold(name, archiveTreeMarker)
}

func hasWindowsArchiveVolumePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func validatePortableArchiveName(name string) error {
	// A single trailing slash is the conventional archive spelling for a
	// directory entry. Everywhere else, empty or dot components are lexical
	// aliases for another destination and are rejected so one payload has one
	// portable path identity across extraction hosts.
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return errors.New("aliases the archive root")
	}
	// A top-level "./" directory entry is emitted by common tar producers.
	// Keep that conventional root marker; dot components inside a real payload
	// path remain forbidden below. archiveEntryRegistry rejects root-as-file.
	if trimmed == "." {
		return nil
	}
	for _, component := range strings.Split(trimmed, "/") {
		if component == "" {
			return errors.New("contains an empty path component")
		}
		if component == "." {
			return errors.New("contains a current-directory component")
		}
		if component == ".." {
			return errors.New("contains a parent-directory component")
		}
		for _, r := range component {
			if r < 0x20 || strings.ContainsRune(`<>"|?*`, r) {
				return fmt.Errorf("contains Windows-invalid character %q", r)
			}
		}
		if strings.Contains(component, ":") {
			return errors.New("contains a Windows alternate-data-stream separator")
		}
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return errors.New("contains a component with a Windows-ambiguous trailing dot or space")
		}
		base := component
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		switch strings.ToUpper(base) {
		case "CON", "PRN", "AUX", "NUL", "CLOCK$",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return fmt.Errorf("contains reserved Windows device name %q", component)
		}
	}
	return nil
}

type archiveEntryKind uint8

const (
	archiveEntryFile archiveEntryKind = iota + 1
	archiveEntryDirectory
)

// archiveEntryRegistry makes archive destination identity independent of the
// host filesystem. In particular, Windows commonly treats path components
// case-insensitively while Unix does not. Without this check an archive with
// both Foo and foo (or with a file that is an ancestor of another entry) could
// materialize differently depending on the installation host.
type archiveEntryRegistry struct {
	entries   map[string]archiveEntryKind
	spellings map[string]string
}

func newArchiveEntryRegistry() *archiveEntryRegistry {
	return &archiveEntryRegistry{
		entries:   make(map[string]archiveEntryKind),
		spellings: make(map[string]string),
	}
}

func (r *archiveEntryRegistry) add(name string, directory bool) error {
	cleanSlash := strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(cleanSlash)
	key := strings.ToLower(clean)
	kind := archiveEntryFile
	if directory {
		kind = archiveEntryDirectory
	}
	if clean == "." && !directory {
		return fmt.Errorf("local archive entry %q aliases the archive root as a file", name)
	}
	if clean != "." {
		parts := strings.Split(clean, "/")
		for i := range parts {
			spelling := strings.Join(parts[:i+1], "/")
			spellingKey := strings.ToLower(spelling)
			if previous, exists := r.spellings[spellingKey]; exists && previous != spelling {
				return fmt.Errorf("local archive entry %q has case-colliding destination component %q (already %q)", name, spelling, previous)
			}
			r.spellings[spellingKey] = spelling
		}
	}
	if previous, exists := r.entries[key]; exists {
		label := "file"
		if previous == archiveEntryDirectory {
			label = "directory"
		}
		return fmt.Errorf("local archive entry %q collides with an existing %s destination", name, label)
	}

	for parent := path.Dir(clean); parent != "." && parent != "/"; parent = path.Dir(parent) {
		if r.entries[strings.ToLower(parent)] == archiveEntryFile {
			return fmt.Errorf("local archive entry %q is nested below an existing file destination %q", name, parent)
		}
	}
	if !directory {
		prefix := key + "/"
		for existing := range r.entries {
			if strings.HasPrefix(existing, prefix) {
				return fmt.Errorf("local archive file entry %q would replace an existing parent directory", name)
			}
		}
	}
	r.entries[key] = kind
	return nil
}
