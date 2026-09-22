package httpdownload

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
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
	name := "artifact"
	if tool != nil && tool.Name != "" {
		name = tool.Name
	}
	return config.ExpandHomeDir(filepath.Join("~/.local/opt", name))
}

func linkTargetDir(mc *config.MethodCandidate, payload string) string {
	if dir, _ := mc.Config["link_dir"].(string); dir != "" {
		return config.ExpandHomeDir(dir)
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
	parent := filepath.Dir(dest)
	requiresElevation := defaultSudoRequired(dest)
	if configured, ok := mc.Config["sudo_required"].(bool); ok {
		requiresElevation = configured
	}
	elevated := requiresElevation && os.Geteuid() != 0
	stageParent := parent
	if elevated {
		if err := elevationGuard(true, tool.Name); err != nil {
			return err
		}
		stageParent = ""
	} else if err := os.MkdirAll(parent, 0o755); err != nil {
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
			_ = os.RemoveAll(raw)
			_ = os.RemoveAll(payload)
		}
		if retErr != nil {
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

	backup := dest + ".depengine-backup"
	if err := commitPayload(ctx, rn, payload, dest, backup, elevated); err != nil {
		return err
	}
	created, err := createLaunchers(ctx, rn, dest, linkTargetDir(mc, dest), points, elevated)
	if err != nil {
		rollbackPayload(ctx, rn, dest, backup, elevated)
		for _, path := range created {
			removeOwned(ctx, rn, path, elevated)
		}
		return err
	}
	removeOwned(ctx, rn, backup, elevated)
	if len(points) > 0 && !pathContains(linkTargetDir(mc, dest)) {
		fmt.Fprintf(os.Stderr, "depengine: add %s to PATH to use %s\n", linkTargetDir(mc, dest), tool.Name)
	}
	committed = true
	return nil
}

func commitPayload(ctx context.Context, rn run.Runner, payload, dest, backup string, elevated bool) error {
	if !elevated {
		_ = os.RemoveAll(backup)
		if _, err := os.Stat(dest); err == nil {
			if err := os.Rename(dest, backup); err != nil {
				return fmt.Errorf("archive: backup existing payload: %w", err)
			}
		}
		if err := os.Rename(payload, dest); err != nil {
			_ = os.Rename(backup, dest)
			return fmt.Errorf("archive: commit payload: %w", err)
		}
		return nil
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "mkdir", "-p", filepath.Dir(dest)), "archive: create destination parent"); err != nil {
		return err
	}
	_ = run.CheckResult(run.RunElevated(ctx, rn, "rm", "-rf", "--", backup), "archive: clear backup")
	if _, err := os.Stat(dest); err == nil {
		if err := run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", dest, backup), "archive: backup payload"); err != nil {
			return err
		}
	}
	if err := run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", payload, dest), "archive: commit payload"); err != nil {
		_ = run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", backup, dest), "archive: restore payload")
		return err
	}
	return nil
}

func rollbackPayload(ctx context.Context, rn run.Runner, dest, backup string, elevated bool) {
	removeOwned(ctx, rn, dest, elevated)
	if elevated {
		_ = run.CheckResult(run.RunElevated(ctx, rn, "mv", "--", backup, dest), "archive: restore payload")
	} else {
		_ = os.Rename(backup, dest)
	}
}

func removeOwned(ctx context.Context, rn run.Runner, path string, elevated bool) {
	if elevated {
		_ = run.CheckResult(run.RunElevated(ctx, rn, "rm", "-rf", "--", path), "archive: remove owned path")
		return
	}
	_ = os.RemoveAll(path)
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

func copyTreeStripped(src, dest string, strip int) error {
	count := 0
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) <= strip {
			return nil
		}
		targetRel := filepath.FromSlash(strings.Join(parts[strip:], "/"))
		target := filepath.Join(dest, targetRel)
		if err := safeJoin(dest, targetRel); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), link))
			if resolved != src && !strings.HasPrefix(resolved, src+string(filepath.Separator)) {
				return fmt.Errorf("archive: symlink %q escapes staging", rel)
			}
			resolvedTarget := filepath.Clean(filepath.Join(filepath.Dir(target), link))
			if resolvedTarget != dest && !strings.HasPrefix(resolvedTarget, dest+string(filepath.Separator)) {
				return fmt.Errorf("archive: symlink %q escapes stripped payload", rel)
			}
			if err := os.Symlink(link, target); err != nil {
				return err
			}
			count++
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
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
	} else if err := os.MkdirAll(linkDir, 0o755); err != nil {
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
			file, err := os.OpenFile(launcher, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
		data, err := os.ReadFile(launcher + ".cmd")
		return err == nil && strings.Contains(string(data), `"`+target+`"`)
	}
	actual, err := os.Readlink(launcher)
	return err == nil && filepath.Clean(actual) == filepath.Clean(target)
}
