// Package source manages package repositories declared on method candidates.
package source

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

// Manager checks and adds candidate-scoped package sources.
type Manager struct {
	rn       run.Runner
	dryRun   bool
	mu       sync.Mutex
	aptDirty bool
}

func NewManager(rn run.Runner, dryRun bool) *Manager { return &Manager{rn: rn, dryRun: dryRun} }

// Ensure makes sources available idempotently. It returns missing sources in
// dry-run mode without mutating the machine.
func (m *Manager) Ensure(ctx context.Context, sources []config.Source) ([]config.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	missing := make([]config.Source, 0, len(sources))
	for _, source := range sources {
		present, err := m.present(ctx, source)
		if err != nil {
			return missing, err
		}
		if present {
			continue
		}
		missing = append(missing, source)
		if m.dryRun {
			continue
		}
		if err := m.add(ctx, source); err != nil {
			return missing, err
		}
		if source.Kind == "apt-ppa" {
			m.aptDirty = true
		}
	}
	if !m.dryRun && m.aptDirty {
		res := run.RunElevated(ctx, m.rn, "apt-get", "update")
		if err := run.CheckResult(res, "apt source update"); err != nil {
			return missing, err
		}
		m.aptDirty = false
	}
	return missing, nil
}

func (m *Manager) present(ctx context.Context, source config.Source) (bool, error) {
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
		result = run.RunElevated(ctx, m.rn, cmd[0], cmd[1:]...)
	} else {
		result = m.rn.Run(ctx, cmd[0], cmd[1:]...)
	}
	return run.CheckResult(result, "source add")
}

func normalizePPA(name string) string {
	if !strings.HasPrefix(name, "ppa:") {
		return name
	}
	return "https://ppa.launchpadcontent.net/" + strings.TrimPrefix(name, "ppa:") + "/ubuntu"
}
