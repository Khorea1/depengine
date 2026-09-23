// Package source manages package repositories declared on method candidates.
package source

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

// Manager checks and adds candidate-scoped package sources.
type Manager struct {
	rn       run.Runner
	mutator  run.Runner
	dryRun   bool
	mu       sync.Mutex
	aptDirty bool
}

func NewManager(rn run.Runner, dryRun bool) *Manager {
	mutator := rn
	if dryRun {
		mutator = run.BlockedRunner{Reason: "dry-run: source mutation is disabled"}
	}
	return &Manager{rn: rn, mutator: mutator, dryRun: dryRun}
}

// EnsureResult distinguishes sources that were merely observed as missing from
// sources whose add operation completed. Callers use Added for compensating
// rollback; a failed add is deliberately not reported as confirmed host state.
type EnsureResult struct {
	Missing     []config.Source
	Added       []config.Source
	Unconfirmed *config.Source
}

// Missing probes candidate sources without mutating host state and returns the
// subset that is currently absent, preserving input order.
func (m *Manager) Missing(ctx context.Context, sources []config.Source) ([]config.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	missing := make([]config.Source, 0, len(sources))
	for _, source := range sources {
		present, err := m.present(ctx, source)
		if err != nil {
			return missing, err
		}
		if !present {
			missing = append(missing, source)
		}
	}
	return missing, nil
}

// Present probes one source without mutating host state. It is exported for
// recovery code that must resolve an in-flight add/remove WAL record from
// read-only evidence.
func (m *Manager) Present(ctx context.Context, source config.Source) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.present(ctx, source)
}

// Add applies one source mutation. Callers that need crash safety must persist
// their write-ahead boundary before invoking Add. The source is assumed to have
// been observed missing; Add intentionally does not perform a second probe that
// could change the ownership classification after a transaction is persisted.
func (m *Manager) Add(ctx context.Context, source config.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.add(ctx, source); err != nil {
		return err
	}
	if source.Kind == "apt-ppa" {
		m.aptDirty = true
	}
	return m.refreshAPT(ctx)
}

// Ensure makes sources available idempotently. It returns missing sources in
// dry-run mode without mutating the machine. Callers that need ownership or
// rollback information should use EnsureTracked.
func (m *Manager) Ensure(ctx context.Context, sources []config.Source) ([]config.Source, error) {
	result, err := m.EnsureTracked(ctx, sources)
	return result.Missing, err
}

// EnsureTracked makes sources available idempotently and reports every source
// whose add operation completed during this call. Added is safe to use as the
// rollback set because pre-existing sources are never included.
func (m *Manager) EnsureTracked(ctx context.Context, sources []config.Source) (EnsureResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := EnsureResult{
		Missing: make([]config.Source, 0, len(sources)),
		Added:   make([]config.Source, 0, len(sources)),
	}
	for _, source := range sources {
		present, err := m.present(ctx, source)
		if err != nil {
			return result, err
		}
		if present {
			continue
		}
		result.Missing = append(result.Missing, source)
		if m.dryRun {
			continue
		}
		if err := m.add(ctx, source); err != nil {
			unconfirmed := source
			result.Unconfirmed = &unconfirmed
			return result, err
		}
		result.Added = append(result.Added, source)
		if source.Kind == "apt-ppa" {
			m.aptDirty = true
		}
	}
	if err := m.refreshAPT(ctx); err != nil {
		return result, err
	}
	return result, nil
}

// Remove removes only the explicitly supplied sources, in reverse order. It is
// idempotent: sources already absent are skipped. Callers must pass only
// sources they own; this method intentionally does not infer ownership.
func (m *Manager) Remove(ctx context.Context, sources []config.Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(sources) - 1; i >= 0; i-- {
		source := sources[i]
		present, err := m.present(ctx, source)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := m.remove(ctx, source); err != nil {
			return err
		}
		if source.Kind == "apt-ppa" {
			m.aptDirty = true
		}
	}
	return m.refreshAPT(ctx)
}

func (m *Manager) refreshAPT(ctx context.Context) error {
	if m.dryRun || !m.aptDirty {
		return nil
	}
	res := run.RunElevated(ctx, m.mutator, "apt-get", "update")
	if err := run.CheckResult(res, "apt source update"); err != nil {
		return err
	}
	m.aptDirty = false
	return nil
}

func (m *Manager) present(ctx context.Context, source config.Source) (bool, error) {
	if err := validateSourceURLSupport(source); err != nil {
		return false, err
	}
	var cmd []string
	switch source.Kind {
	case "apt-ppa":
		cmd = []string{"add-apt-repository", "--list"}
	case "dnf-copr":
		cmd = []string{"dnf", "copr", "list", "--enabled"}
	case "scoop-bucket":
		cmd = []string{"scoop", "bucket", "list"}
	case "brew-tap":
		cmd = []string{"brew", "tap"}
	default:
		return false, fmt.Errorf("source: unsupported kind %q", source.Kind)
	}
	res := m.rn.Run(ctx, cmd[0], cmd[1:]...)
	if err := run.CheckResult(res, "source check"); err != nil {
		return false, err
	}
	want := source.Name
	if source.Kind == "apt-ppa" {
		want = normalizePPA(want)
	}
	return strings.Contains(strings.ToLower(string(res.Stdout)), strings.ToLower(want)), nil
}

func (m *Manager) add(ctx context.Context, source config.Source) error {
	if err := validateSourceURLSupport(source); err != nil {
		return err
	}
	var cmd []string
	switch source.Kind {
	case "apt-ppa":
		cmd = []string{"add-apt-repository", "--yes", "--no-update", source.Name}
	case "dnf-copr":
		cmd = []string{"dnf", "copr", "enable", "-y", source.Name}
	case "scoop-bucket":
		cmd = []string{"scoop", "bucket", "add", source.Name}
		if source.URL != "" {
			cmd = append(cmd, source.URL)
		}
	case "brew-tap":
		cmd = []string{"brew", "tap", source.Name}
		if source.URL != "" {
			cmd = append(cmd, source.URL)
		}
	}
	var result run.Result
	if source.Kind == "apt-ppa" || source.Kind == "dnf-copr" {
		result = run.RunElevated(ctx, m.mutator, cmd[0], cmd[1:]...)
	} else {
		result = m.mutator.Run(ctx, cmd[0], cmd[1:]...)
	}
	return run.CheckResult(result, "source add")
}

func validateSourceURLSupport(source config.Source) error {
	if source.URL != "" && (source.Kind == "apt-ppa" || source.Kind == "dnf-copr") {
		return fmt.Errorf("source: URL is unsupported for kind %q", source.Kind)
	}
	return nil
}

func (m *Manager) remove(ctx context.Context, source config.Source) error {
	var cmd []string
	switch source.Kind {
	case "apt-ppa":
		cmd = []string{"add-apt-repository", "--yes", "--remove", "--no-update", source.Name}
	case "dnf-copr":
		cmd = []string{"dnf", "copr", "remove", "-y", source.Name}
	case "scoop-bucket":
		cmd = []string{"scoop", "bucket", "rm", source.Name}
	case "brew-tap":
		cmd = []string{"brew", "untap", source.Name}
	default:
		return fmt.Errorf("source: unsupported kind %q", source.Kind)
	}
	var result run.Result
	if source.Kind == "apt-ppa" || source.Kind == "dnf-copr" {
		result = run.RunElevated(ctx, m.mutator, cmd[0], cmd[1:]...)
	} else {
		result = m.mutator.Run(ctx, cmd[0], cmd[1:]...)
	}
	return run.CheckResult(result, "source remove")
}

func normalizePPA(name string) string {
	if !strings.HasPrefix(name, "ppa:") {
		return name
	}
	return "https://ppa.launchpadcontent.net/" + strings.TrimPrefix(name, "ppa:") + "/ubuntu"
}
