package httpdownload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/config"
)

const archiveOwnershipMarkerName = ".depengine-owner.json"

const archiveOwnershipMarkerFormat = 1

type archiveOwnershipMarker struct {
	Format     int    `json:"format"`
	Tool       string `json:"tool"`
	MethodKind string `json:"method_kind"`
}

func expectedArchiveOwnership(tool *config.Tool, mc *config.MethodCandidate) (archiveOwnershipMarker, error) {
	if tool == nil || tool.Name == "" {
		return archiveOwnershipMarker{}, fmt.Errorf("archive: tool identity is required for payload ownership")
	}
	if mc == nil {
		return archiveOwnershipMarker{}, fmt.Errorf("archive: method identity is required for payload ownership")
	}
	return archiveOwnershipMarker{
		Format:     archiveOwnershipMarkerFormat,
		Tool:       tool.Name,
		MethodKind: mc.Kind,
	}, nil
}

func writeArchiveOwnership(rootPath string, owner archiveOwnershipMarker) error {
	root, err := os.OpenRoot(rootPath) // #nosec G304 -- rootPath is a depengine-created private payload staging directory.
	if err != nil {
		return fmt.Errorf("archive: open payload ownership root: %w", err)
	}
	defer func() { _ = root.Close() }()

	if _, err := root.Lstat(archiveOwnershipMarkerName); err == nil {
		return fmt.Errorf("archive: payload contains reserved ownership marker %s", archiveOwnershipMarkerName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("archive: inspect ownership marker: %w", err)
	}

	f, err := root.OpenFile(archiveOwnershipMarkerName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G304 -- fixed filename below a rooted staging directory.
	if err != nil {
		return fmt.Errorf("archive: create ownership marker: %w", err)
	}
	encErr := json.NewEncoder(f).Encode(owner)
	syncErr := f.Sync()
	closeErr := f.Close()
	if encErr != nil {
		return fmt.Errorf("archive: encode ownership marker: %w", encErr)
	}
	if syncErr != nil {
		return fmt.Errorf("archive: sync ownership marker: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("archive: close ownership marker: %w", closeErr)
	}
	return nil
}

func verifyArchiveOwnership(rootPath string, expected archiveOwnershipMarker) error {
	info, err := os.Lstat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("archive: owned payload root %s is not a real directory", rootPath)
	}
	root, err := os.OpenRoot(rootPath) // #nosec G304 -- rootPath is the exact install destination selected by the tracked method.
	if err != nil {
		return fmt.Errorf("archive: open owned payload root: %w", err)
	}
	defer func() { _ = root.Close() }()

	markerInfo, err := root.Lstat(archiveOwnershipMarkerName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("archive: ownership marker is missing")
		}
		return fmt.Errorf("archive: inspect ownership marker: %w", err)
	}
	if !markerInfo.Mode().IsRegular() {
		return fmt.Errorf("archive: ownership marker is not a regular file")
	}
	if markerInfo.Size() > 4096 {
		return fmt.Errorf("archive: ownership marker is unexpectedly large")
	}
	f, err := root.Open(archiveOwnershipMarkerName)
	if err != nil {
		return fmt.Errorf("archive: open ownership marker: %w", err)
	}
	defer func() { _ = f.Close() }()

	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	var actual archiveOwnershipMarker
	if err := decoder.Decode(&actual); err != nil {
		return fmt.Errorf("archive: decode ownership marker: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("archive: ownership marker contains trailing JSON")
		}
		return fmt.Errorf("archive: decode ownership marker trailer: %w", err)
	}
	if actual.Format != archiveOwnershipMarkerFormat || actual.Tool != expected.Tool || actual.MethodKind != expected.MethodKind {
		return fmt.Errorf("archive: ownership marker belongs to tool %q method %q, not tool %q method %q", actual.Tool, actual.MethodKind, expected.Tool, expected.MethodKind)
	}
	return nil
}

func requireOwnedArchivePathOrAbsent(path string, expected archiveOwnershipMarker, purpose string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("archive: inspect %s %s: %w", purpose, path, err)
	}
	if err := verifyArchiveOwnership(path, expected); err != nil {
		return fmt.Errorf("archive: refusing to replace %s %s without matching ownership: %w", purpose, filepath.Clean(path), err)
	}
	return nil
}
