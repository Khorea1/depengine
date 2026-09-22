package httpdownload

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/artifact"
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/downloadcache"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// HTTPAdapter implements exec.Adapter for HTTP(S) downloads.
// Supports archive extraction, checksum verification, and {latest}
// resolution via GitHub releases API.
type HTTPAdapter struct{}

// NewHTTPAdapter creates an HTTP download adapter.
func NewHTTPAdapter() *HTTPAdapter {
	return &HTTPAdapter{}
}

func (a *HTTPAdapter) Kind() string { return "http" }

// ResolvePlan performs the same artifact resolution Install uses, but without
// downloading or mutating anything. This makes dry-run an auditable resolved
// plan rather than only a method-selection preview.
func (a *HTTPAdapter) ResolvePlan(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	return resolveDownloadPlan(ctx, rn, mc, intent)
}

func resolveDownloadPlan(ctx context.Context, rn run.Runner, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, nil
	}
	resolvedURL, version, err := ResolveArtifactDetails(ctx, mc, rn)
	if err != nil {
		return intent, err
	}
	resolved := intent.Clone()
	if version == "" {
		version, _ = mc.Config["_resolved_version"].(string)
	}
	if version != "" && resolved.Identity.Version == "" {
		resolved.Identity.Version = version
	}
	if len(resolved.Artifacts) == 0 {
		resolved.Artifacts = []plan.Artifact{{URL: resolvedURL}}
	} else {
		resolved.Artifacts[0].URL = resolvedURL
	}
	return &resolved, nil
}

// RequiresElevation applies the same path-derived default and explicit
// sudo_required override used by Install.
func (a *HTTPAdapter) RequiresElevation(tool *config.Tool, mc *config.MethodCandidate) bool {
	extractTo := "/usr/local/bin"
	if configured, ok := mc.Config["extract_to"].(string); ok && configured != "" {
		extractTo = configured
	}
	artifactName := stringConfig(mc, "url")
	if artifactName == "" {
		artifactName = stringConfig(mc, "asset")
	}
	if isArchive(fileExtension(artifactName)) {
		extractTo = archiveTarget(tool, mc)
	} else if scopeConfigured(mc) {
		extractTo = PlacementOrDefault(tool, mc, "/usr/local/bin", "").InstallRoot
	}
	extractTo = config.ExpandHomeDir(extractTo)
	required := defaultSudoRequired(extractTo)
	if configured, ok := mc.Config["sudo_required"].(bool); ok {
		required = configured
	}
	if isArchive(fileExtension(artifactName)) && len(entrypoints(mc)) > 0 {
		linkDir := linkTargetDir(mc, extractTo, tool)
		required = required || defaultSudoRequired(linkDir)
	}
	return required
}

// Available returns true — Go net/http is always available; curl/wget
// are detected lazily on actual download.
func (a *HTTPAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return true
}

// Check verifies if the tool appears already installed. The check is
// file-based, never directory-based:
//   - extract_to configured → the extracted TARGET FILE must exist inside it
//     (extract_to/<binary> when the install record names a binary, otherwise
//     extract_to/<tool name>). A bare directory is NOT proof of installation:
//     an unrelated operation (e.g. another tool's clone) can create the
//     directory while this tool's file was never downloaded.
//   - binary configured without extract_to → binary must be reachable on PATH.
func (a *HTTPAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	extractTo, _ := mc.Config["extract_to"].(string)
	if extractTo == "" && scopeConfigured(mc) {
		// A scoped install targets the scope's platform-native install
		// root, so presence is checked there — never guessed from PATH or a
		// legacy /usr/local/bin default.
		extractTo = PlacementOrDefault(tool, mc, "", "").InstallRoot
	}
	extractTo = config.ExpandHomeDir(extractTo)
	binary, _ := mc.Config["binary"].(string)
	if len(entrypoints(mc)) > 0 {
		payload := archiveTarget(tool, mc)
		for name, relative := range entrypoints(mc) {
			if requirePayloadFile(payload, relative) != nil {
				return false
			}
			if !launcherValid(payload, linkTargetDir(mc, payload, tool), name, relative) {
				return false
			}
		}
		return true
	}

	if extractTo != "" {
		target := binary
		if target == "" {
			if tool == nil {
				return false
			}
			target = tool.Name
		}
		info, err := os.Stat(filepath.Join(extractTo, target))
		return err == nil && info.Mode().IsRegular()
	}

	if binary != "" {
		return run.LookPath(ctx, rn, binary)
	}
	return false
}

// Observe reports whether the tool is already installed, reaching the same
// verdict as Check on every detection strategy (owned-archive entrypoints,
// extract_to/<binary or tool name> as a regular file, binary on PATH). A
// present observation carries the tool name as known package identity so it
// reconciles with the resolved plan. An installed version is deliberately
// never reported: depengine cannot determine a version from a bare file on
// disk, so echoing the desired version here would fake verification.
func (a *HTTPAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("http: tool and method are required")
	}
	if !a.Check(ctx, rn, tool, mc) {
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	observation := plan.Observation{Presence: plan.PresencePresent}
	if tool.Name != "" {
		observation.Identity = plan.ObservedIdentity{Package: tool.Name}
		observation.KnownFields = []plan.IdentityField{plan.FieldPackage}
	}
	return observation, nil
}

// Install downloads a file from URL, optionally verifies its checksum,
// and extracts it based on file type.
func (a *HTTPAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	resolvedURL, err := ResolveArtifact(ctx, mc, rn)
	if err != nil {
		return fmt.Errorf("http: resolve artifact: %w", err)
	}
	return a.installResolvedURL(ctx, rn, tool, mc, resolvedURL)
}

// InstallResolved executes an already-resolved download plan. It never calls
// ResolveArtifact, ResolveArtifactDetails, ResolveLatest, or any release API:
// the concrete URL must already be present in resolved.Artifacts[0].URL.
func (a *HTTPAdapter) InstallResolved(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil || len(resolved.Artifacts) == 0 || resolved.Artifacts[0].URL == "" {
		return fmt.Errorf("http: resolved plan has no concrete artifact URL")
	}
	return a.installResolvedURL(ctx, rn, tool, mc, resolved.Artifacts[0].URL)
}

func (a *HTTPAdapter) installResolvedURL(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate, resolvedURL string) error {
	// Re-enforce the shared artifact URL contract at the runtime boundary.
	// Normal CLI flows validate before execution, but adapters are also public
	// package APIs and must not leak embedded credentials when called directly.
	if err := artifact.ValidateURL(resolvedURL, []string{"http", "https"}); err != nil {
		return fmt.Errorf("http: artifact URL: %w", err)
	}

	// Determine file extension and re-enforce the same artifact-format contract
	// semantic validation uses. _allow_installer is reserved for dedicated
	// adapters that intentionally delegate transport to HTTPAdapter.
	ext := fileExtension(resolvedURL)
	allowInstaller, _ := mc.Config["_allow_installer"].(bool)
	if contract, ok := methodkind.Lookup("http"); ok && contract.Artifact != nil {
		if err := contract.Artifact.ValidateArtifact(resolvedURL); err != nil {
			var forbidden *artifact.ForbiddenExtensionError
			switch {
			case !errors.As(err, &forbidden):
				return fmt.Errorf("http: artifact: %w", err)
			case allowInstaller:
				// Dedicated installer adapter owns the execution semantics.
			case forbidden.Extension == ".msi":
				return fmt.Errorf("http: %s is a platform installer; use the msi method", forbidden.Extension)
			default:
				return fmt.Errorf("http: %s is a platform installer; no dedicated installer method is available for this format", forbidden.Extension)
			}
		}
	}
	tmpDir, err := os.MkdirTemp("", "depengine-http-*")
	if err != nil {
		return fmt.Errorf("http: temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Determine the actual filename from the download URL.
	fileName := resolvedFileName(resolvedURL, ext)
	tmpFile := tmpDir + "/" + fileName

	// --- Download cache ---
	// Check if the file is already cached by its resolved URL.
	cachedPath := downloadcache.Lookup(resolvedURL)
	fromCache := false

	if cachedPath != "" {
		// Copy the cached file to the temp location.
		if err := copyLocalFile(cachedPath, tmpFile); err != nil {
			// Cache read error is non-fatal — fall through to download.
		} else {
			fromCache = true
		}
	}

	if !fromCache {
		// Download from remote.
		dl := SelectDownloaderForURL(ctx, rn, resolvedURL)
		if err := retryWithBackoff(ctx, 3, time.Second, 10*time.Second, func(retryCtx context.Context) error {
			return dl.Download(retryCtx, resolvedURL, tmpFile)
		}); err != nil {
			return fmt.Errorf("http: download %s: %w", tool.Name, downloadErrorWithHint(err))
		}
	}

	// Verify checksum if configured.
	if checksum, ok := mc.Config["checksum"].(string); ok && checksum != "" {
		if err := a.verifyChecksum(ctx, rn, tmpFile, resolvedURL, checksum, mc.Config); err != nil {
			// If we used a cached file and checksum fails, re-download fresh.
			if fromCache {
				log.Default.Warn("cached copy failed checksum, re-downloading", "tool", tool.Name)
				downloadcache.Remove(resolvedURL)
				dl := SelectDownloaderForURL(ctx, rn, resolvedURL)
				if err2 := retryWithBackoff(ctx, 3, time.Second, 10*time.Second, func(retryCtx context.Context) error {
					return dl.Download(retryCtx, resolvedURL, tmpFile)
				}); err2 != nil {
					return fmt.Errorf("http: download %s (re-download): %w", tool.Name, err2)
				}
				// Retry checksum verification on fresh download.
				if err2 := a.verifyChecksum(ctx, rn, tmpFile, resolvedURL, checksum, mc.Config); err2 != nil {
					return fmt.Errorf("http: checksum: %w", err2)
				}
			} else {
				return fmt.Errorf("http: checksum: %w", err)
			}
		}
	}

	// sudo is path-derived: destinations under $HOME are user-writable and
	// need no elevation (e.g. ~/.local/share/fonts); everything else (the
	// /usr/local/bin default, /opt, …) keeps sudo. Explicit sudo_required
	// in the schema always wins.
	sudoRequired := a.RequiresElevation(tool, mc)
	binary, _ := mc.Config["binary"].(string)
	if isArchive(ext) {
		if err := installArchive(ctx, tmpFile, ext, tool, mc, rn); err != nil {
			return fmt.Errorf("http: extract: %w", err)
		}
	} else {
		// Raw binary: placement decides the destination. Explicit
		// extract_to wins; a scoped candidate defaults to the scope's
		// platform-native install root; otherwise /usr/local/bin.
		placement, err := ArtifactPlacement(tool, mc, "/usr/local/bin", "")
		if err != nil {
			return err
		}
		if err := extract(ctx, tmpFile, placement.InstallRoot, ext, binary, rn, sudoRequired, tool.Name); err != nil {
			return fmt.Errorf("http: extract: %w", err)
		}
		// A scoped raw binary needs a link in the scope link dir to be
		// reachable from PATH, mirroring archive entrypoints. Scope-less
		// candidates keep the historical behavior (the binary lands
		// directly in extract_to, which must already be on PATH).
		if scopeConfigured(mc) && placement.LinkDir != "" {
			name := binary
			if name == "" && tool != nil {
				name = tool.Name
			}
			if name != "" {
				linkElevated := defaultSudoRequired(placement.LinkDir) && os.Geteuid() != 0
				if _, err := createLaunchers(ctx, rn, placement.InstallRoot, placement.LinkDir, map[string]string{name: name}, linkElevated); err != nil {
					return fmt.Errorf("http: link: %w", err)
				}
			}
		}
	}

	// Store in cache after extraction (Store may move tmpFile via os.Rename).
	// Re-storing a cache hit is intentional: checksum recovery may have removed
	// the stale entry and downloaded a fresh copy while fromCache remains true.
	if _, err := downloadcache.Store(resolvedURL, tmpFile); err != nil {
		// Cache write failure is non-fatal; the install continues.
		log.Default.Warn("cache write failed", "error", err, "url", resolvedURL)
	}

	return nil
}

// resolvedFileName derives the downloaded file's name from an already-
// {latest}-resolved URL, falling back to "download"+ext when the URL has no
// usable path segment (e.g. a bare host, or a query-only URL). URL paths
// always use "/" separators, so the "path" package (not "path/filepath")
// is required for correct behavior on Windows.
func resolvedFileName(resolvedURL, ext string) string {
	fileName := "download" + ext
	if parsedURL, err := url.Parse(resolvedURL); err == nil && parsedURL.Path != "" {
		if base := path.Base(parsedURL.Path); base != "" && base != "." && base != "/" {
			if path.Ext(base) == "" {
				base += ext
			}
			fileName = base
		}
	}
	return fileName
}

// checksumConfig holds parsed checksum-related configuration.
type checksumConfig struct {
	algorithm string // "sha256", "md5", "sha1", "sha512"
	url       string // explicit checksum URL from checksum_url config
	format    string // "sha256sum", "bsd", or "raw" from checksum_file_format config
}

// extractChecksumConfig extracts checksum-related config from a checksum string
// and method config.
func extractChecksumConfig(checksum string, config map[string]any) *checksumConfig {
	_, algorithm, err := parseChecksumPrefix(checksum)
	if err != nil {
		return nil
	}

	cc := &checksumConfig{algorithm: algorithm}
	if v, ok := config["checksum_url"].(string); ok {
		cc.url = v
	}
	if v, ok := config["checksum_file_format"].(string); ok {
		cc.format = v
	}
	return cc
}

// detectAlgorithmFromURL detects the checksum algorithm from a checksum URL
// filename. Returns empty string if no algorithm can be determined.
func detectAlgorithmFromURL(checksumURL string) string {
	base := strings.ToUpper(path.Base(checksumURL))
	switch {
	case strings.Contains(base, "SHA256"):
		return "sha256"
	case strings.Contains(base, "SHA512"):
		return "sha512"
	case strings.Contains(base, "SHA1"):
		return "sha1"
	case strings.Contains(base, "MD5"):
		return "md5"
	}
	return ""
}

// verifyChecksum resolves checksum verification. When the checksum string
// ends with ":auto", it tries to resolve the hash from a companion checksum
// file using config-driven URL and format options.
func (a *HTTPAdapter) verifyChecksum(ctx context.Context, rn run.Runner, filePath, downloadURL, checksum string, config map[string]any) error {
	// Handle :auto suffix — resolve checksum from a companion file.
	if strings.HasSuffix(checksum, ":auto") {
		cc := extractChecksumConfig(checksum, config)
		if cc == nil {
			return fmt.Errorf("http: checksum: invalid checksum format: %q", checksum)
		}
		return a.resolveAutoChecksum(ctx, rn, filePath, downloadURL, cc, config)
	}

	// Plain checksum — verify directly.
	return VerifyChecksum(filePath, checksum)
}

// resolveAutoChecksum handles :auto checksum resolution by trying to fetch
// a companion checksum file and extracting the expected hash.
func (a *HTTPAdapter) resolveAutoChecksum(ctx context.Context, rn run.Runner, filePath, downloadURL string, cc *checksumConfig, config map[string]any) error {
	log.Default.Warn("checksum fetched from server (TOFU)", "algorithm", cc.algorithm, "hint", "use checksum_url for a separate source, or pin the hash in depengine.lock")

	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("%s:auto: invalid download URL %q: %w", cc.algorithm, downloadURL, err)
	}
	wantName := path.Base(parsedURL.Path)
	if wantName == "" || wantName == "." || wantName == "/" {
		return fmt.Errorf("%s:auto: cannot determine filename from URL %q", cc.algorithm, downloadURL)
	}

	// Build the list of checksum URLs to try.
	var checksumURLs []string
	if cc.url != "" {
		checksumURLs = []string{cc.url}
	} else {
		// Try companion URL patterns.
		dir := ""
		if idx := strings.LastIndex(parsedURL.Path, "/"); idx >= 0 {
			dir = parsedURL.Path[:idx]
		}
		baseURL := parsedURL.Scheme + "://" + parsedURL.Host
		algoUpper := strings.ToUpper(cc.algorithm)
		checksumURLs = []string{
			downloadURL + "." + cc.algorithm,
			baseURL + dir + "/" + algoUpper + "SUMS",
			baseURL + dir + "/checksums.txt",
		}
	}

	var lastErr error
	for _, checksumURL := range checksumURLs {
		resolvedHash, err := a.fetchChecksumFromURL(ctx, rn, checksumURL, wantName, cc, config)
		if err != nil {
			lastErr = err
			continue
		}
		// Store resolved checksum in config so the lockfile mechanism
		// can capture the pinned hash later.
		if _, ok := config["_checksum_resolved"]; !ok {
			config["_checksum_resolved"] = cc.algorithm + ":" + resolvedHash
		}
		return VerifyChecksum(filePath, cc.algorithm+":"+resolvedHash)
	}
	return fmt.Errorf("%s:auto: could not resolve checksum: %w", cc.algorithm, lastErr)
}

// fetchChecksumFromURL downloads a checksum file from the given URL and
// extracts the hash for the wanted filename.
func (a *HTTPAdapter) fetchChecksumFromURL(ctx context.Context, rn run.Runner, checksumURL, wantName string, cc *checksumConfig, config map[string]any) (string, error) {
	tmpDir, err := os.MkdirTemp("", "depengine-checksum-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	checksumFile := tmpDir + "/checksum"
	dl := SelectDownloaderForURL(ctx, rn, checksumURL)
	if err := dl.Download(ctx, checksumURL, checksumFile); err != nil {
		return "", fmt.Errorf("downloading %s: %w", checksumURL, err)
	}

	// --- GPG signature verification of checksum file ---
	if sigURL, ok := config["signature_url"].(string); ok && sigURL != "" {
		signingKey, _ := config["signing_key"].(string)
		sigFile := tmpDir + "/checksum.sig"
		sigDownloader := SelectDownloaderForURL(ctx, rn, sigURL)
		if err := sigDownloader.Download(ctx, sigURL, sigFile); err != nil {
			return "", fmt.Errorf("downloading signature %s: %w", sigURL, err)
		}
		if err := GPGVerify(ctx, rn, checksumFile, sigFile, signingKey); err != nil {
			return "", fmt.Errorf("gpg: %w", err)
		}
	}

	// If format is "raw", the entire file content is the hash.
	if cc.format == "raw" {
		data, err := os.ReadFile(checksumFile)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", checksumURL, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	f, err := os.Open(checksumFile)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var checksums map[string]string
	switch cc.format {
	case "bsd":
		checksums, err = ParseChecksumFileBSDExtended(f)
	case "sha256sum":
		checksums, err = ParseChecksumFile(f)
	default:
		checksums, err = ParseChecksumFileAuto(f)
	}
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", checksumURL, err)
	}

	hash, ok := checksums[wantName]
	if !ok {
		return "", fmt.Errorf("no checksum for %q in %s", wantName, checksumURL)
	}
	return hash, nil
}

// Ensure HTTPAdapter implements exec.Adapter.
// CheckAvailable assumes availability: download URLs have no cheap local
// index to probe, so an unreachable artifact surfaces at download time.
func (a *HTTPAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

var _ exec.Adapter = (*HTTPAdapter)(nil)
var _ exec.AdapterV2 = (*HTTPAdapter)(nil)

// isSharedDir checks if a directory path is a common shared system directory.
// We avoid deleting these directories completely during uninstallation.
// Separators are folded to "/" after Clean so Unix-style manifests and
// Windows-style paths evaluate identically on every platform (ToSlash
// alone is a no-op for literal backslashes on Unix).
func isSharedDir(path string) bool {
	p := strings.ReplaceAll(filepath.Clean(path), "\\", "/")
	if p == "/" || p == "." {
		return true
	}
	shared := []string{
		"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/usr/local/sbin",
		"/opt", "/usr", "/usr/local", "/lib", "/usr/lib", "/usr/local/lib",
		"C:/Windows", "C:/Program Files", "C:/Program Files (x86)",
	}
	for _, s := range shared {
		if p == s {
			return true
		}
	}
	if strings.HasSuffix(p, "/bin") || strings.HasSuffix(p, "/sbin") {
		return true
	}
	return false
}

// Remove uninstalls an HTTP-installed tool. If extract_to is a shared
// directory (e.g. /usr/local/bin), only the extracted binary is removed.
// If extract_to is tool-specific, the entire directory is deleted.
// Without extract_to, removal is not supported — the download was extracted
// to the default /usr/local/bin, which is shared, so we remove the binary or
// the tool name from there.
func (a *HTTPAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if len(entrypoints(mc)) > 0 {
		payload := archiveTarget(tool, mc)
		payloadElevated := defaultSudoRequired(payload) && os.Geteuid() != 0
		linkDir := linkTargetDir(mc, payload, tool)
		linkElevated := defaultSudoRequired(linkDir) && os.Geteuid() != 0
		for name, relative := range entrypoints(mc) {
			launcher := filepath.Join(linkDir, name)
			if runtime.GOOS == "windows" {
				launcher += ".cmd"
			}
			if _, err := os.Lstat(launcher); err == nil && !launcherValid(payload, linkDir, name, relative) {
				return fmt.Errorf("http: refusing to remove launcher %s because it no longer targets the owned payload", launcher)
			}
			if err := removeHTTPPath(ctx, rn, launcher, false, linkElevated); err != nil {
				return fmt.Errorf("http: remove launcher: %w", err)
			}
		}
		if isSharedDir(payload) {
			return fmt.Errorf("http: refusing to remove shared archive destination %s", payload)
		}
		if err := removeHTTPPath(ctx, rn, payload, true, payloadElevated); err != nil {
			return fmt.Errorf("http: remove payload: %w", err)
		}
		return nil
	}
	extractTo, _ := mc.Config["extract_to"].(string)
	if extractTo == "" && scopeConfigured(mc) {
		extractTo = PlacementOrDefault(tool, mc, "/usr/local/bin", "").InstallRoot
	}
	extractTo = config.ExpandHomeDir(extractTo)
	if extractTo == "" {
		extractTo = "/usr/local/bin" // Install default
	}

	binary, _ := mc.Config["binary"].(string)
	target := binary
	if target == "" {
		if tool == nil {
			return fmt.Errorf("http: remove not supported — no extract_to and no tool name")
		}
		target = tool.Name
	}

	// Scope installs also own the PATH link created at install time. Remove
	// the link before the payload directory so a stale link never outlives
	// its target.
	if scopeConfigured(mc) {
		placement := PlacementOrDefault(tool, mc, "/usr/local/bin", "")
		if placement.LinkDir != "" {
			launcher := filepath.Join(placement.LinkDir, target)
			if runtime.GOOS == "windows" {
				launcher += ".cmd"
			}
			if _, err := os.Lstat(launcher); err == nil && !launcherValid(extractTo, placement.LinkDir, target, target) {
				return fmt.Errorf("http: refusing to remove launcher %s because it no longer targets the owned payload", launcher)
			}
			linkElevated := defaultSudoRequired(placement.LinkDir) && os.Geteuid() != 0
			if err := removeHTTPPath(ctx, rn, launcher, false, linkElevated); err != nil {
				return fmt.Errorf("http: remove link: %w", err)
			}
		}
	}

	payloadElevated := defaultSudoRequired(extractTo) && os.Geteuid() != 0
	if isSharedDir(extractTo) {
		path := filepath.Join(extractTo, target)
		if err := removeHTTPPath(ctx, rn, path, false, payloadElevated); err != nil {
			return fmt.Errorf("http: remove %s: %w", path, err)
		}
		return nil
	}

	// Not a shared directory — safe to delete the whole directory
	if err := removeHTTPPath(ctx, rn, extractTo, true, payloadElevated); err != nil {
		return fmt.Errorf("http: remove directory %s: %w", extractTo, err)
	}
	return nil
}

func removeHTTPPath(ctx context.Context, rn run.Runner, path string, recursive, elevated bool) error {
	var err error
	if recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
	}
	if err == nil {
		return nil
	}
	if !elevated {
		return err
	}
	args := []string{"-f", "--", path}
	if recursive {
		args = []string{"-rf", "--", path}
	}
	return run.CheckResult(run.RunElevated(ctx, rn, "rm", args...), "remove owned path")
}

// CanRemove returns true — the adapter can remove installations done via
// HTTP download when extract_to and/or binary is configured.
func (a *HTTPAdapter) CanRemove() bool { return true }

// copyLocalFile copies a file from src to dst, preserving permissions.
// Used by the download cache to materialize cached files into temp locations.
func copyLocalFile(src, dst string) error {
	return downloadcache.CopyFile(src, dst)
}
