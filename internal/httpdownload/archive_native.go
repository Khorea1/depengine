package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	archiveExpansionLimit int64 = 4 << 30
	archiveEntryLimit           = 100_000
	archivePathByteLimit        = 4096
	archivePathDepthLimit       = 256
)

type archiveExpansionBudget struct {
	used    int64
	entries int
}

func (b *archiveExpansionBudget) accountEntry(name string) error {
	if b.entries >= archiveEntryLimit {
		return fmt.Errorf("archive entry count exceeds %d-entry limit", archiveEntryLimit)
	}
	if len(name) > archivePathByteLimit {
		return fmt.Errorf("archive entry name exceeds %d-byte limit", archivePathByteLimit)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed != "" && strings.Count(trimmed, "/")+1 > archivePathDepthLimit {
		return fmt.Errorf("archive entry path exceeds %d-component depth limit", archivePathDepthLimit)
	}
	b.entries++
	return nil
}

func (b *archiveExpansionBudget) writer(dst io.Writer) io.Writer {
	return archiveBudgetWriter{dst: dst, budget: b}
}

type archiveBudgetWriter struct {
	dst    io.Writer
	budget *archiveExpansionBudget
}

func (w archiveBudgetWriter) Write(p []byte) (int, error) {
	remaining := archiveExpansionLimit - w.budget.used
	if remaining <= 0 {
		return 0, fmt.Errorf("archive regular-file expansion exceeds %d-byte limit", archiveExpansionLimit)
	}
	if int64(len(p)) > remaining {
		n, err := w.dst.Write(p[:int(remaining)])
		w.budget.used += int64(n)
		if err != nil {
			return n, err
		}
		return n, fmt.Errorf("archive regular-file expansion exceeds %d-byte limit", archiveExpansionLimit)
	}
	n, err := w.dst.Write(p)
	w.budget.used += int64(n)
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// safeArchiveRelative converts an archive's portable slash-separated member
// name into a host-relative path suitable for os.Root. Backslashes and
// drive-qualified names are rejected so the same archive has the same path
// identity on Unix and Windows.
func safeArchiveRelative(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("invalid archive entry name")
	}
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("archive entry %q must use portable '/' separators", name)
	}
	if path.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute path in archive entry: %q", name)
	}
	if hasArchiveWindowsVolumePrefix(name) {
		return "", fmt.Errorf("drive-qualified path in archive entry: %q", name)
	}

	cleanSlash := path.Clean(name)
	if cleanSlash == ".." || strings.HasPrefix(cleanSlash, "../") {
		return "", fmt.Errorf("entry escapes destination: %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(cleanSlash))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry escapes destination: %q", name)
	}
	return clean, nil
}

func hasArchiveWindowsVolumePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func validateArchiveLinkTarget(entryName, linkName string, symlink bool) error {
	if linkName == "" || strings.ContainsRune(linkName, '\x00') {
		return fmt.Errorf("archive entry %q has invalid link target", entryName)
	}
	if strings.Contains(linkName, `\`) {
		return fmt.Errorf("archive entry %q link target %q must use portable '/' separators", entryName, linkName)
	}
	if path.IsAbs(linkName) || strings.HasPrefix(linkName, "/") || hasArchiveWindowsVolumePrefix(linkName) {
		return fmt.Errorf("archive entry %q links outside destination to %q", entryName, linkName)
	}

	resolved := path.Clean(linkName)
	if symlink {
		entryPath := path.Clean(entryName)
		resolved = path.Clean(path.Join(path.Dir(entryPath), linkName))
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") || path.IsAbs(resolved) {
		return fmt.Errorf("archive entry %q link target escapes destination: %q", entryName, linkName)
	}
	_, err := safeArchiveRelative(resolved)
	return err
}

type rootedArchiveMaterializer struct {
	root           *os.Root
	budget         archiveExpansionBudget
	directoryModes map[string]os.FileMode
}

func openRootedArchiveMaterializer(dest string) (*rootedArchiveMaterializer, error) {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return nil, fmt.Errorf("open archive destination root: %w", err)
	}
	return &rootedArchiveMaterializer{
		root:           root,
		directoryModes: make(map[string]os.FileMode),
	}, nil
}

func (m *rootedArchiveMaterializer) close() error {
	if m == nil || m.root == nil {
		return nil
	}
	return m.root.Close()
}

func archiveFileMode(mode os.FileMode) os.FileMode {
	mode = mode.Perm()
	if mode == 0 {
		return 0o644
	}
	return mode
}

func archiveDirectoryMode(mode os.FileMode) os.FileMode {
	mode = mode.Perm()
	if mode == 0 {
		return 0o755
	}
	return mode
}

func tarArchiveMode(mode int64) (os.FileMode, error) {
	if mode < 0 || mode > 0o7777 {
		return 0, fmt.Errorf("mode %d is outside the supported permission range", mode)
	}
	return os.FileMode(mode & 0o777), nil
}

func (m *rootedArchiveMaterializer) mkdir(name string, mode os.FileMode) error {
	mode = archiveDirectoryMode(mode)
	if err := m.root.MkdirAll(name, mode|0o700); err != nil {
		return err
	}
	m.directoryModes[name] = mode
	return nil
}

func (m *rootedArchiveMaterializer) ensureParent(name string) error {
	parent := filepath.Dir(name)
	if parent == "." {
		return nil
	}
	return m.root.MkdirAll(parent, 0o755)
}

func (m *rootedArchiveMaterializer) writeFile(ctx context.Context, name string, mode os.FileMode, src io.Reader, exactSize int64) error {
	if err := m.ensureParent(name); err != nil {
		return err
	}
	out, err := m.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, archiveFileMode(mode))
	if err != nil {
		return err
	}

	reader := contextReader{ctx: ctx, r: src}
	var copyErr error
	if exactSize >= 0 {
		_, copyErr = io.CopyN(m.budget.writer(out), reader, exactSize)
	} else {
		_, copyErr = io.Copy(m.budget.writer(out), reader) // #nosec G110 -- aggregate expanded bytes are capped by archiveExpansionLimit.
	}
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = m.root.Remove(name)
		return errors.Join(copyErr, closeErr)
	}
	return nil
}

func (m *rootedArchiveMaterializer) symlink(name, target string) error {
	if err := m.ensureParent(name); err != nil {
		return err
	}
	return m.root.Symlink(filepath.FromSlash(target), name)
}

func (m *rootedArchiveMaterializer) hardlink(name, target string) error {
	if err := m.ensureParent(name); err != nil {
		return err
	}
	return m.root.Link(target, name)
}

func (m *rootedArchiveMaterializer) applyDirectoryModes() error {
	paths := make([]string, 0, len(m.directoryModes))
	for name := range m.directoryModes {
		paths = append(paths, name)
	}
	sort.Slice(paths, func(i, j int) bool {
		depthI := strings.Count(filepath.Clean(paths[i]), string(filepath.Separator))
		depthJ := strings.Count(filepath.Clean(paths[j]), string(filepath.Separator))
		if depthI != depthJ {
			return depthI > depthJ
		}
		return paths[i] < paths[j]
	})
	for _, name := range paths {
		if err := m.root.Chmod(name, m.directoryModes[name]); err != nil {
			return fmt.Errorf("set final archive directory permissions %q: %w", name, err)
		}
	}
	return nil
}

func extractNativeZip(ctx context.Context, src, dest string) (retErr error) {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("zip: open %s: %w", src, err)
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("zip: close %s: %w", src, closeErr)
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("zip: stat %s: %w", src, err)
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return fmt.Errorf("zip: open archive: %w", err)
	}
	m, err := openRootedArchiveMaterializer(dest)
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	defer func() {
		if closeErr := m.close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("zip: close destination root: %w", closeErr)
		}
	}()

	for _, entry := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := m.budget.accountEntry(entry.Name); err != nil {
			return fmt.Errorf("zip entry %q: %w", entry.Name, err)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip entry %q is a symlink and is not supported", entry.Name)
		}
		target, err := safeArchiveRelative(entry.Name)
		if err != nil {
			return fmt.Errorf("unsafe zip entry: %w", err)
		}
		if entry.FileInfo().IsDir() {
			if err := m.mkdir(target, entry.Mode()); err != nil {
				return fmt.Errorf("zip: create directory %q: %w", entry.Name, err)
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("zip entry %q is not a regular file", entry.Name)
		}
		rc, err := entry.Open()
		if err != nil {
			return fmt.Errorf("zip: open entry %q: %w", entry.Name, err)
		}
		writeErr := m.writeFile(ctx, target, entry.Mode(), rc, -1)
		closeErr := rc.Close()
		if writeErr != nil || closeErr != nil {
			return errors.Join(
				wrapNativeArchiveEntryError("extract", entry.Name, writeErr),
				wrapNativeArchiveEntryError("close", entry.Name, closeErr),
			)
		}
	}
	if err := m.applyDirectoryModes(); err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	return nil
}

func extractNativeTar(ctx context.Context, src, dest string) (retErr error) {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("tar: open %s: %w", src, err)
	}
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("tar: close %s: %w", src, closeErr)
		}
	}()

	m, err := openRootedArchiveMaterializer(dest)
	if err != nil {
		return fmt.Errorf("tar: %w", err)
	}
	defer func() {
		if closeErr := m.close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("tar: close destination root: %w", closeErr)
		}
	}()

	tr := tar.NewReader(f)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: read archive: %w", err)
		}
		if err := m.budget.accountEntry(hdr.Name); err != nil {
			return fmt.Errorf("tar entry %q: %w", hdr.Name, err)
		}
		target, err := safeArchiveRelative(hdr.Name)
		if err != nil {
			return fmt.Errorf("unsafe tar entry: %w", err)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			mode, err := tarArchiveMode(hdr.Mode)
			if err != nil {
				return fmt.Errorf("tar directory %q: %w", hdr.Name, err)
			}
			if err := m.mkdir(target, mode); err != nil {
				return fmt.Errorf("tar: create directory %q: %w", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 {
				return fmt.Errorf("tar entry %q has negative size", hdr.Name)
			}
			mode, err := tarArchiveMode(hdr.Mode)
			if err != nil {
				return fmt.Errorf("tar file %q: %w", hdr.Name, err)
			}
			if err := m.writeFile(ctx, target, mode, tr, hdr.Size); err != nil {
				return fmt.Errorf("tar: extract entry %q: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			if err := validateArchiveLinkTarget(hdr.Name, hdr.Linkname, true); err != nil {
				return fmt.Errorf("unsafe tar entry: %w", err)
			}
			if err := m.symlink(target, hdr.Linkname); err != nil {
				return fmt.Errorf("tar: create symlink %q: %w", hdr.Name, err)
			}
		case tar.TypeLink:
			if err := validateArchiveLinkTarget(hdr.Name, hdr.Linkname, false); err != nil {
				return fmt.Errorf("unsafe tar entry: %w", err)
			}
			linkTarget, err := safeArchiveRelative(hdr.Linkname)
			if err != nil {
				return fmt.Errorf("unsafe tar entry: %w", err)
			}
			if err := m.hardlink(target, linkTarget); err != nil {
				return fmt.Errorf("tar: create hardlink %q: %w", hdr.Name, err)
			}
		default:
			return fmt.Errorf("tar entry %q has unsupported type %d", hdr.Name, hdr.Typeflag)
		}
	}
	if err := m.applyDirectoryModes(); err != nil {
		return fmt.Errorf("tar: %w", err)
	}
	return nil
}

func wrapNativeArchiveEntryError(action, name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s archive entry %q: %w", action, name, err)
}
