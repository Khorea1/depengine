package httpdownload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
)

func isArchive(ext string) bool {
	switch ext {
	case ".tar.gz", ".tgz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tar", ".zip", ".bz2":
		return true
	}
	return false
}

func entrypoints(mc *config.MethodCandidate) map[string]string {
	raw, _ := mc.Config["entrypoints"].(map[string]any)
	out := make(map[string]string, len(raw))
	for name, value := range raw {
		if path, ok := value.(string); ok {
			out[name] = path
		}
	}
	return out
}

func archiveTarget(tool *config.Tool, mc *config.MethodCandidate) string {
	if target, _ := mc.Config["extract_to"].(string); target != "" {
		return config.ExpandHomeDir(target)
	}
	// A scoped archive installs into the scope's platform-native install
	// root; the entrypoint links make the payload reachable from PATH.
	if scopeConfigured(mc) {
		if root := PlacementOrDefault(tool, mc, "", "").InstallRoot; root != "" {
			return root
		}
	}
	name := "artifact"
	if tool != nil && tool.Name != "" {
		name = tool.Name
	}
	return config.ExpandHomeDir(filepath.Join("~/.local/opt", name))
}

func linkTargetDir(mc *config.MethodCandidate, payload string, tool *config.Tool) string {
	if dir, _ := mc.Config["link_dir"].(string); dir != "" {
		return config.ExpandHomeDir(dir)
	}
	if scopeConfigured(mc) {
		if link := PlacementOrDefault(tool, mc, "", "").LinkDir; link != "" {
			return link
		}
	}
	if !defaultSudoRequired(payload) {
		return config.ExpandHomeDir("~/.local/bin")
	}
	return "/usr/local/bin"
}

func installArchive(ctx context.Context, src, ext string, tool *config.Tool, mc *config.MethodCandidate, rn run.Runner) (retErr error) {
	dest := archiveTarget(tool, mc)
	if isSharedDir(dest) {
		return fmt.Errorf("archive: extract_to %s must be a tool-owned directory, not a shared directory", dest)
	}
	owner, err := expectedArchiveOwnership(tool, mc)
	if err != nil {
		return err
	}
	backup := dest + ".depengine-backup"
	if err := requireOwnedArchivePathOrAbsent(dest, owner, "destination"); err != nil {
		return err
	}
	if err := requireOwnedArchivePathOrAbsent(backup, owner, "backup"); err != nil {
		return err
	}
	parent := filepath.Dir(dest)
	requiresElevation := defaultSudoRequired(dest)
	if configured, ok := mc.Config["sudo_required"].(bool); ok {
		requiresElevation = configured
	}
	payloadElevated := requiresElevation && os.Geteuid() != 0
	stageParent := parent
	if payloadElevated {
		if err := elevationGuard(true, tool.Name); err != nil {
			return err
		}
		stageParent = ""
	} else if err := os.MkdirAll(parent, 0o755); err != nil { // #nosec G301 -- Installation parents are intentionally traversable for installed payloads.
		return fmt.Errorf("archive: create destination parent: %w", err)
	}
	raw, err := os.MkdirTemp(stageParent, ".depengine-raw-*")
	if err != nil {
		return fmt.Errorf("archive: staging: %w", err)
	}
	payload, err := os.MkdirTemp(stageParent, ".depengine-payload-*")
	if err != nil {
		return fmt.Errorf("archive: staging %s: %w", raw, err)
	}
	committed := false
	defer func() {
		if committed {
			if err := os.RemoveAll(raw); err != nil {
				log.Default.Warn("archive staging cleanup failed after commit", "path", raw, "error", err)
			}
			if err := os.RemoveAll(payload); err != nil {
				log.Default.Warn("archive staging cleanup failed after commit", "path", payload, "error", err)
			}
		}
		if retErr != nil && !committed {
			retErr = fmt.Errorf("%w (staging preserved at %s and %s)", retErr, raw, payload)
		}
	}()
	if err := extract(ctx, src, raw, ext, "", rn, false, tool.Name); err != nil {
		return err
	}
	strip := 0
	if value, ok := mc.Config["strip_components"].(int64); ok {
		strip = int(value)
	}
	if strip < 0 {
		return fmt.Errorf("archive: strip_components must be non-negative")
	}
	if err := copyTreeStripped(raw, payload, strip); err != nil {
		return err
	}
	entries, err := os.ReadDir(payload)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("archive: strip_components=%d produced an empty payload", strip)
	}
	if binary, _ := mc.Config["binary"].(string); binary != "" {
		if err := requirePayloadFile(payload, binary); err != nil {
			return fmt.Errorf("archive: binary: %w", err)
		}
	}
	points := entrypoints(mc)
	for name, relative := range points {
		if name == "" || filepath.Base(name) != name {
			return fmt.Errorf("archive: invalid entrypoint name %q", name)
		}
		if err := requirePayloadFile(payload, relative); err != nil {
			return fmt.Errorf("archive: entrypoint %s: %w", name, err)
		}
	}
	if err := writeArchiveOwnership(payload, owner); err != nil {
		return err
	}

	if err := commitPayload(ctx, rn, payload, dest, backup, payloadElevated); err != nil {
		return err
	}
	// A process already running as root does not cross the elevation boundary,
	// but its private staging root is still mode 0700. Normalize the committed
	// root before exposing launchers; the staging tree is already root-owned.
	if requiresElevation && !payloadElevated && os.Geteuid() == 0 {
		if err := os.Chmod(dest, 0o755); err != nil { // #nosec G302 -- system payload roots must remain traversable after private staging.
			return errors.Join(fmt.Errorf("archive: normalize root payload mode: %w", err), rollbackPayload(ctx, rn, dest, backup, false))
		}
	}
	linkDir := linkTargetDir(mc, dest, tool)
	linkElevated := defaultSudoRequired(linkDir) && os.Geteuid() != 0
	created, err := createLaunchers(ctx, rn, dest, linkDir, points, linkElevated)
	if err != nil {
		errs := []error{err, rollbackPayload(ctx, rn, dest, backup, payloadElevated)}
		for _, path := range created {
			errs = append(errs, removeOwned(ctx, rn, path, linkElevated))
		}
		return errors.Join(errs...)
	}
	committed = true
	if err := removeOwned(ctx, rn, backup, payloadElevated); err != nil {
		log.Default.Warn("archive backup cleanup failed after commit", "path", backup, "error", err)
	}
	if len(points) > 0 && !pathContains(linkDir) {
		fmt.Fprintf(os.Stderr, "depengine: add %s to PATH to use %s\n", linkDir, tool.Name)
	}
	return nil
}

func commitPayload(ctx context.Context, rn run.Runner, payload, dest, backup string, elevated bool) error {
	if !elevated {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("archive: clear backup: %w", err)
		}
		hadPayload := false
		if _, err := os.Stat(dest); err == nil {
			if err := os.Rename(dest, backup); err != nil {
				return fmt.Errorf("archive: backup existing payload: %w", err)
			}
			hadPayload = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("archive: inspect destination: %w", err)
		}
		if err := os.Rename(payload, dest); err != nil {
			primary := fmt.Errorf("archive: commit payload: %w", err)
			if hadPayload {
				primary = errors.Join(primary, rollbackPayload(ctx, rn, dest, backup, false))
			}
			return primary
		}
		return nil
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "mkdir", "-p", filepath.Dir(dest)), "archive: create destination parent"); err != nil {
		return err
	}
	ownerPaths, err := payloadOwnerPaths(payload, dest)
	if err != nil {
		return err
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "rm", "-rf", "--", backup), "archive: clear backup"); err != nil {
		return err
	}
	hadPayload := false
	if _, err := os.Stat(dest); err == nil {
		if err := run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", dest, backup), "archive: backup payload"); err != nil {
			return err
		}
		hadPayload = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("archive: inspect destination: %w", err)
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", payload, dest), "archive: commit payload"); err != nil {
		primary := err
		if hadPayload {
			primary = errors.Join(primary, rollbackPayload(ctx, rn, dest, backup, true))
		}
		return primary
	}
	// Staging roots and members are created by the invoking user. Normalize
	// exactly the paths materialized in staging. WalkDir never follows symlinks,
	// and chown -h changes a symlink itself rather than its external target.
	modePath, err := filepath.Abs(dest)
	if err != nil {
		return errors.Join(fmt.Errorf("archive: resolve payload path: %w", err), rollbackCommittedPayload(ctx, rn, dest, backup, hadPayload, true))
	}
	if err := normalizeElevatedPayloadOwner(ctx, rn, ownerPaths); err != nil {
		return errors.Join(err, rollbackCommittedPayload(ctx, rn, dest, backup, hadPayload, true))
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "chmod", "0755", modePath), "archive: normalize payload mode"); err != nil {
		return errors.Join(err, rollbackCommittedPayload(ctx, rn, dest, backup, hadPayload, true))
	}
	return nil
}

func payloadOwnerPaths(payload, dest string) ([]string, error) {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return nil, fmt.Errorf("archive: resolve payload destination: %w", err)
	}
	paths := make([]string, 0, 32)
	err = filepath.WalkDir(payload, func(current string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(payload, current)
		if err != nil {
			return err
		}
		if relative == "." {
			paths = append(paths, absDest)
			return nil
		}
		paths = append(paths, filepath.Join(absDest, relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("archive: enumerate payload ownership paths: %w", err)
	}
	return paths, nil
}

func normalizeElevatedPayloadOwner(ctx context.Context, rn run.Runner, paths []string) error {
	const ownershipBatchSize = 128
	for start := 0; start < len(paths); start += ownershipBatchSize {
		end := min(start+ownershipBatchSize, len(paths))
		args := make([]string, 0, 2+end-start)
		args = append(args, "-h", "0")
		args = append(args, paths[start:end]...)
		if err := run.CheckResult(run.RunElevated(ctx, rn, "chown", args...), "archive: normalize payload owner"); err != nil {
			return err
		}
	}
	return nil
}

func rollbackPayload(ctx context.Context, rn run.Runner, dest, backup string, elevated bool) error {
	removeErr := removeOwned(ctx, rn, dest, elevated)
	if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		return removeErr
	} else if err != nil {
		return errors.Join(removeErr, fmt.Errorf("archive: inspect backup for restore: %w", err))
	}
	var restoreErr error
	if elevated {
		restoreErr = run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", backup, dest), "archive: restore payload")
	} else if err := os.Rename(backup, dest); err != nil {
		restoreErr = fmt.Errorf("archive: restore payload: %w", err)
	}
	return errors.Join(removeErr, restoreErr)
}

func rollbackCommittedPayload(ctx context.Context, rn run.Runner, dest, backup string, hadPayload, elevated bool) error {
	removeErr := removeOwned(ctx, rn, dest, elevated)
	if !hadPayload {
		return removeErr
	}
	var restoreErr error
	if elevated {
		restoreErr = run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", backup, dest), "archive: restore payload")
	} else if err := os.Rename(backup, dest); err != nil {
		restoreErr = fmt.Errorf("archive: restore payload: %w", err)
	}
	return errors.Join(removeErr, restoreErr)
}

func removeOwned(ctx context.Context, rn run.Runner, path string, elevated bool) error {
	if elevated {
		return run.CheckResult(run.RunElevated(ctx, rn, "rm", "-rf", "--", path), "archive: remove owned path")
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("archive: remove owned path %s: %w", path, err)
	}
	return nil
}

func pathContains(dir string) bool {
	want := filepath.Clean(dir)
	for _, item := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(item) == want {
			return true
		}
	}
	return false
}

func requirePayloadFile(root, relative string) error {
	if filepath.IsAbs(relative) {
		return fmt.Errorf("path %q must be relative", relative)
	}
	if err := safeJoin(root, relative); err != nil {
		return err
	}
	info, err := os.Stat(filepath.Join(root, filepath.Clean(relative)))
	if err != nil {
		return fmt.Errorf("%q does not exist: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", relative)
	}
	return nil
}

func symlinkTargetStaysWithinRoot(relative, target string) bool {
	if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
		return false
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(relative), target))
	return resolved != ".." && !strings.HasPrefix(resolved, ".."+string(filepath.Separator))
}

func copyTreeStripped(src, dest string, strip int) error {
	sourceRoot, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("archive: open staging root: %w", err)
	}
	defer func() { _ = sourceRoot.Close() }()
	targetRoot, err := os.OpenRoot(dest)
	if err != nil {
		return fmt.Errorf("archive: open payload root: %w", err)
	}
	defer func() { _ = targetRoot.Close() }()

	count := 0
	err = fs.WalkDir(sourceRoot.FS(), ".", func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		sourceRel := filepath.FromSlash(path)
		parts := strings.Split(path, "/")
		if len(parts) <= strip {
			return nil
		}
		targetRel := filepath.FromSlash(strings.Join(parts[strip:], "/"))
		if err := safeJoin(dest, targetRel); err != nil {
			return err
		}

		info, err := sourceRoot.Lstat(sourceRel)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return targetRoot.MkdirAll(targetRel, info.Mode().Perm())
		}
		if err := targetRoot.MkdirAll(filepath.Dir(targetRel), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := sourceRoot.Readlink(sourceRel)
			if err != nil {
				return err
			}
			if !symlinkTargetStaysWithinRoot(sourceRel, link) {
				return fmt.Errorf("archive: symlink %q escapes staging", sourceRel)
			}
			if !symlinkTargetStaysWithinRoot(targetRel, link) {
				return fmt.Errorf("archive: symlink %q escapes stripped payload", sourceRel)
			}
			if err := targetRoot.Symlink(link, targetRel); err != nil {
				return err
			}
			count++
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archive: unsupported staged entry %q (%s)", sourceRel, info.Mode().Type())
		}

		in, err := sourceRoot.Open(sourceRel)
		if err != nil {
			return err
		}
		out, err := targetRoot.OpenFile(targetRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return fmt.Errorf("archive: conflicting stripped path %q: %w", targetRel, err)
		}
		_, copyErr := io.Copy(out, in)
		inErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if closeErr != nil {
			return closeErr
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("archive: strip_components=%d produced an empty payload", strip)
	}
	return nil
}

func createLaunchers(ctx context.Context, rn run.Runner, payload, linkDir string, points map[string]string, elevated bool) ([]string, error) {
	if len(points) == 0 {
		return nil, nil
	}
	if elevated {
		if err := run.CheckResult(run.RunElevated(ctx, rn, "mkdir", "-p", linkDir), "archive: create link_dir"); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(linkDir, 0o755); err != nil { // #nosec G301 -- Launcher directories must be traversable so installed commands can execute.
		return nil, fmt.Errorf("archive: create link_dir: %w", err)
	}
	names := make([]string, 0, len(points))
	for name := range points {
		names = append(names, name)
	}
	sort.Strings(names)
	created := make([]string, 0, len(names))
	for _, name := range names {
		target := filepath.Join(payload, filepath.Clean(points[name]))
		launcher := filepath.Join(linkDir, name)
		if launcherValid(payload, linkDir, name, points[name]) {
			continue
		}
		if runtime.GOOS == "windows" {
			launcher += ".cmd"
			content := []byte("@echo off\r\n\"" + target + "\" %*\r\n")
			file, err := os.OpenFile(launcher, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755) // #nosec G304,G302 -- launcher is path-validated and must be executable on Windows.
			if err == nil {
				_, err = file.Write(content)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				return created, fmt.Errorf("archive: create shim %s: %w", launcher, err)
			}
		} else {
			if elevated {
				if _, err := os.Lstat(launcher); err == nil {
					return created, fmt.Errorf("archive: launcher %s already exists and is not owned by this payload", launcher)
				}
				if err := run.CheckResult(run.RunElevated(ctx, rn, "ln", "-s", "--", target, launcher), "archive: create symlink"); err != nil {
					return created, err
				}
			} else {
				if err := os.Symlink(target, launcher); err != nil {
					return created, fmt.Errorf("archive: create symlink %s: %w", launcher, err)
				}
			}
		}
		created = append(created, launcher)
	}
	return created, nil
}

func launcherValid(payload, linkDir, name, relative string) bool {
	target := filepath.Join(payload, filepath.Clean(relative))
	launcher := filepath.Join(linkDir, name)
	if runtime.GOOS == "windows" {
		data, err := os.ReadFile(launcher + ".cmd") // #nosec G304 -- launcher is derived from the validated link directory and configured launcher name.
		return err == nil && strings.Contains(string(data), `"`+target+`"`)
	}
	actual, err := os.Readlink(launcher)
	return err == nil && filepath.Clean(actual) == filepath.Clean(target)
}
