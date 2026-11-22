package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
)

// replacementBackupSuffix names the sibling that holds the tracked target
// while an upgrade builds and verifies its replacement. It is deterministic so
// a later run can adopt an orphan left by a crash.
const replacementBackupSuffix = ".depengine-old"

var errNoReplacementTarget = errors.New("git: no removable replacement target")

// QuarantineForReplacement detaches the tracked installation instead of
// deleting it. The replacement is built and verified while the old target is
// still available at a sibling path; rollback restores it on any failure and
// commit removes it after the new state is durable.
func (a *GitAdapter) QuarantineForReplacement(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (func() error, func() error, error) {
	if runtime.GOOS == "windows" {
		// A running executable cannot be renamed on Windows. Falling back to
		// the ordinary removal path keeps existing upgrade behavior instead of
		// failing every Git upgrade on that platform.
		return nil, nil, exec.ErrQuarantineUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	targets, err := replacementTargets(mc)
	if err != nil {
		if errors.Is(err, errNoReplacementTarget) {
			return nil, nil, exec.ErrQuarantineUnsupported
		}
		return nil, nil, err
	}
	tx := &quarantineTransaction{}
	for _, target := range targets {
		pair, err := quarantineTarget(target)
		if err != nil {
			_ = tx.rollback()
			return nil, nil, err
		}
		if pair != nil {
			tx.pairs = append(tx.pairs, *pair)
		}
	}
	return tx.commit, tx.rollback, nil
}

func replacementTargets(mc *config.MethodCandidate) ([]string, error) {
	if mc == nil {
		return nil, errNoReplacementTarget
	}
	if paths, err := managedPaths(mc); err != nil {
		return nil, err
	} else if len(paths) > 0 {
		return paths, nil
	}
	extractTo, _ := mc.Config["extract_to"].(string)
	if extractTo == "" {
		return nil, errNoReplacementTarget
	}
	extractTo = filepath.Clean(config.ExpandHomeDir(extractTo))
	if !isSharedDir(extractTo) {
		return []string{extractTo}, nil
	}
	binary, _ := mc.Config["binary"].(string)
	if binary == "" || binary != filepath.Base(binary) || binary == "." || binary == ".." {
		return nil, errNoReplacementTarget
	}
	return []string{filepath.Join(extractTo, binary)}, nil
}

type quarantinePair struct {
	target string
	backup string
}

type quarantineTransaction struct {
	pairs []quarantinePair
	done  bool
}

// quarantineTarget renames target to its deterministic backup. A leftover
// backup next to a live target is treated as the residue of a completed
// replacement and discarded before the current target is detached.
func quarantineTarget(target string) (*quarantinePair, error) {
	target = filepath.Clean(target)
	backup := target + replacementBackupSuffix
	if _, err := os.Lstat(target); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("git: inspect replacement target %s: %w", target, err)
		}
		if _, err := os.Lstat(backup); err == nil {
			return &quarantinePair{target: target, backup: backup}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("git: inspect replacement backup %s: %w", backup, err)
		}
		return nil, nil
	}
	if err := os.RemoveAll(backup); err != nil {
		return nil, fmt.Errorf("git: clear stale replacement backup %s: %w", backup, err)
	}
	if err := os.Rename(target, backup); err != nil {
		return nil, fmt.Errorf("git: detach replacement target %s: %w", target, err)
	}
	return &quarantinePair{target: target, backup: backup}, nil
}

func (tx *quarantineTransaction) commit() error {
	if tx.done {
		return nil
	}
	tx.done = true
	var firstErr error
	for _, pair := range tx.pairs {
		if err := os.RemoveAll(pair.backup); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = fmt.Errorf("git: remove replacement backup %s: %w", pair.backup, err)
		}
	}
	return firstErr
}

func (tx *quarantineTransaction) rollback() error {
	if tx.done {
		return nil
	}
	tx.done = true
	var firstErr error
	for i := len(tx.pairs) - 1; i >= 0; i-- {
		pair := tx.pairs[i]
		if err := os.RemoveAll(pair.target); err != nil && !errors.Is(err, os.ErrNotExist) {
			if firstErr == nil {
				firstErr = fmt.Errorf("git: clear partial replacement %s: %w", pair.target, err)
			}
			continue
		}
		if err := os.Rename(pair.backup, pair.target); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("git: restore replacement target %s: %w", pair.target, err)
			}
		}
	}
	return firstErr
}

// removeReplacementBackup clears a backup left by an interrupted upgrade after
// a successful install. It never fails the install: the new target is already
// in place and a stale sibling is harmless.
func removeReplacementBackup(target string) {
	_ = os.RemoveAll(filepath.Clean(target) + replacementBackupSuffix)
}
