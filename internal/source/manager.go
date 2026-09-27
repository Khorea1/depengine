// Package source manages package repositories declared on method candidates.
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
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
	ctx = omitSourceSecretEnvironment(ctx, sources)
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
	ctx = omitSourceSecretEnvironment(ctx, []config.Source{source})
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.present(ctx, source)
}

// Add applies one source mutation. Callers that need crash safety must persist
// their write-ahead boundary before invoking Add. The source is assumed to have
// been observed missing; Add intentionally does not perform a second probe that
// could change the ownership classification after a transaction is persisted.
func (m *Manager) Add(ctx context.Context, source config.Source) error {
	return m.AddAuthenticated(ctx, source, "")
}

// AddAuthenticated uses a bearer token only for the source add subprocess.
// The token never enters the source descriptor or its durable resource key.
func (m *Manager) AddAuthenticated(ctx context.Context, source config.Source, token string) error {
	ctx = omitSourceSecretEnvironment(ctx, []config.Source{source})
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.addAuthenticated(ctx, source, token); err != nil {
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
	ctx = omitSourceSecretEnvironment(ctx, sources)
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
	ctx = omitSourceSecretEnvironment(ctx, sources)
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

func omitSourceSecretEnvironment(ctx context.Context, sources []config.Source) context.Context {
	var names []string
	for _, source := range sources {
		if source.SecretRef != nil && source.SecretRef.Provider == "env" && source.SecretRef.Name != "" {
			names = append(names, source.SecretRef.Name)
		}
	}
	return run.WithOmittedEnv(ctx, names...)
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
	if source.Kind == "brew-tap" && source.URL != "" {
		return m.brewTapPresent(ctx, source)
	}
	if source.Kind == "scoop-bucket" && source.URL != "" {
		return m.scoopBucketPresent(ctx, source)
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

func (m *Manager) brewTapPresent(ctx context.Context, source config.Source) (bool, error) {
	res := m.rn.Run(ctx, "brew", "tap")
	if err := run.CheckResult(res, "source check"); err != nil {
		return false, err
	}
	if !outputHasSourceName(string(res.Stdout), source.Name) {
		return false, nil
	}

	info := m.rn.Run(ctx, "brew", "tap-info", "--json=v1", source.Name)
	if err := run.CheckResult(info, "source origin check"); err != nil {
		return false, err
	}
	var taps []struct {
		Name   string `json:"name"`
		Remote string `json:"remote"`
	}
	if err := json.Unmarshal(info.Stdout, &taps); err != nil {
		return false, fmt.Errorf("source: verify brew-tap %s origin: decode tap metadata: %w", source.Name, err)
	}
	for _, tap := range taps {
		if !strings.EqualFold(tap.Name, source.Name) {
			continue
		}
		if tap.Remote == "" {
			return false, fmt.Errorf("source: verify brew-tap %s origin: tap metadata has no remote", source.Name)
		}
		if !sameSourceURL(tap.Remote, source.URL) {
			return false, fmt.Errorf("source: brew-tap %s exists with a different origin than configured", source.Name)
		}
		return true, nil
	}
	return false, fmt.Errorf("source: verify brew-tap %s origin: tap metadata omitted the configured tap", source.Name)
}

func (m *Manager) scoopBucketPresent(ctx context.Context, source config.Source) (bool, error) {
	res := m.rn.Run(ctx, "scoop", "bucket", "list")
	if err := run.CheckResult(res, "source check"); err != nil {
		return false, err
	}
	want := strings.TrimSpace(source.Name)
	found := false
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], want) {
			continue
		}
		found = true
		if sameSourceURL(fields[1], source.URL) {
			return true, nil
		}
	}
	if !found {
		return false, nil
	}
	return false, fmt.Errorf("source: scoop-bucket %s exists with a different origin than configured", source.Name)
}

func outputHasSourceName(output, name string) bool {
	_, ok := outputSourceLine(output, name)
	return ok
}

func outputSourceLine(output, name string) (string, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, want) {
			continue
		}
		if len(lower) == len(want) || (len(lower) > len(want) && (lower[len(want)] == ' ' || lower[len(want)] == '\t')) {
			return trimmed, true
		}
	}
	return "", false
}

func sameSourceURL(actual, expected string) bool {
	return canonicalSourceURL(actual) == canonicalSourceURL(expected)
}

func canonicalSourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return strings.TrimSuffix(raw, "/")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSuffix(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	switch u.Hostname() {
	case "github.com", "gitlab.com", "codeberg.org":
		u.Path = strings.TrimSuffix(u.Path, ".git")
	}
	return u.String()
}

func (m *Manager) add(ctx context.Context, source config.Source) error {
	return m.addAuthenticated(ctx, source, "")
}

func (m *Manager) addAuthenticated(ctx context.Context, source config.Source, token string) error {
	if err := validateSourceURLSupport(source); err != nil {
		return err
	}
	if source.SecretRef != nil && token == "" {
		return fmt.Errorf("source: credential is required for %s %s", source.Kind, source.Name)
	}
	if source.SecretRef == nil && token != "" {
		return fmt.Errorf("source: unexpected credential for %s %s", source.Kind, source.Name)
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
	} else if token != "" {
		env, err := gitBearerEnv(source.URL, token)
		if err != nil {
			return err
		}
		result = run.RunWithEnv(ctx, m.mutator, env, []string{token}, cmd[0], cmd[1:]...)
	} else {
		result = m.mutator.Run(ctx, cmd[0], cmd[1:]...)
	}
	return run.CheckResult(result, "source add")
}

func validateSourceURLSupport(source config.Source) error {
	if source.URL != "" && (source.Kind == "apt-ppa" || source.Kind == "dnf-copr") {
		return fmt.Errorf("source: URL is unsupported for kind %q", source.Kind)
	}
	if source.URL != "" {
		if strings.TrimSpace(source.URL) != source.URL || strings.ContainsRune(source.URL, '\x00') {
			return fmt.Errorf("source: URL must not contain surrounding whitespace or NUL for kind %q", source.Kind)
		}
		if strings.Contains(source.URL, "://") {
			u, err := url.Parse(source.URL)
			if err != nil || u.Scheme == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("source: URL must be credential-free and must not contain query or fragment for kind %q", source.Kind)
			}
		} else if strings.ContainsAny(source.URL, "?#") {
			return fmt.Errorf("source: URL must not contain query or fragment for kind %q", source.Kind)
		}
	}
	if source.SecretRef != nil {
		if source.Kind != "scoop-bucket" && source.Kind != "brew-tap" {
			return fmt.Errorf("source: secret_ref is unsupported for kind %q", source.Kind)
		}
		u, err := url.Parse(source.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("source: secret_ref requires a credential-free HTTPS URL for kind %q", source.Kind)
		}
	}
	return nil
}

func gitBearerEnv(rawURL, token string) (map[string]string, error) {
	if strings.ContainsAny(token, " \t\r\n\x00") {
		return nil, fmt.Errorf("source: credential is invalid for Git HTTP bearer authentication")
	}
	count := 0
	if current := os.Getenv("GIT_CONFIG_COUNT"); current != "" {
		var err error
		count, err = strconv.Atoi(current)
		if err != nil || count < 0 || count > 1024 {
			return nil, fmt.Errorf("source: inherited Git configuration count is invalid")
		}
	}
	index := strconv.Itoa(count)
	return map[string]string{
		"GIT_CONFIG_COUNT":          strconv.Itoa(count + 1),
		"GIT_CONFIG_KEY_" + index:   "http." + rawURL + ".extraheader",
		"GIT_CONFIG_VALUE_" + index: "Authorization: Bearer " + token,
		"GIT_TERMINAL_PROMPT":       "0",
	}, nil
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
