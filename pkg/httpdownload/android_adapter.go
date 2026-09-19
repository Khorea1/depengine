package httpdownload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// androidAPKDir is the fixed, non-configurable location where a resolved
// .apk waits to be handed to Android's package installer. Not exposed as a
// schema field (no "install_dir" for this kind, unlike "appimage"): a
// schema author has no reason to relocate it, and a fixed path is what
// keeps Check() simple — the same stable filename every re-run looks for.
const androidAPKDir = "~/.cache/depengine/android-apks"

// AndroidAdapter implements exec.Adapter for the "android" method kind:
// resolves a URL exactly like "http"/"appimage" do ({latest}/{version}
// placeholders, checksum verification, retry/cache) via HTTPAdapter, then
// does the one thing HTTPAdapter can't: hand the downloaded .apk to
// Android's own package installer through `termux-open` (part of the
// termux-api package; requires the companion Termux:API app to be
// installed too). Runs entirely inside Termux, no SSH, no new Runner.
//
// Config fields:
//
//	url (required) same meaning as on "http"/"appimage".
//
// Every other "http" field (checksum, checksum_url, signing_key, ...) has
// the exact same meaning, because the download itself is delegated to
// HTTPAdapter unchanged. There is deliberately no "binary"/"install_dir"
// field: the .apk always lands under androidAPKDir named "<tool>.apk" —
// it is never meant to end up on PATH.
//
// IMPORTANT — what "installed" means here: success means "handed to the
// Android package installer", not "installed". The human still has to tap
// through the installer's prompt, asynchronously and outside depengine's
// process. Check() reflects this: it can only confirm the .apk was
// downloaded and dispatched, never that the app is actually present on the
// system (Termux has no reliable `pm list packages` without root/adb). This
// is a documented limitation, not a bug.
type AndroidAdapter struct {
	http *HTTPAdapter
}

// NewAndroidAdapter creates an "android" adapter, delegating download,
// checksum and retry logic to an HTTPAdapter.
func NewAndroidAdapter() *AndroidAdapter {
	return &AndroidAdapter{http: NewHTTPAdapter()}
}

func (a *AndroidAdapter) Kind() string { return "android" }

// Available requires both the Termux environment itself (cheap, dependency-
// free signal: $PREFIX is set by every Termux shell and essentially never
// set elsewhere) and termux-open on PATH. Checking $PREFIX first avoids a
// false positive from an unrelated same-named script on a normal Linux box.
func (a *AndroidAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if os.Getenv("PREFIX") == "" {
		return false
	}
	return run.LookPath(ctx, rn, "termux-open")
}

// apkTarget resolves the (dir, filename) pair the downloaded .apk always
// lands at for a given tool — fixed, not schema-configurable (see
// androidAPKDir).
func apkTarget(tool *config.Tool) (dir, name string) {
	if tool == nil || tool.Name == "" {
		return config.ExpandHomeDir(androidAPKDir), ""
	}
	return config.ExpandHomeDir(androidAPKDir), tool.Name + ".apk"
}

// Check delegates to HTTPAdapter.Check against the fixed (dir, name) pair —
// the same "does the target file exist" logic "http"/"appimage" already
// implement. See the type doc comment for what this can and cannot promise.
func (a *AndroidAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	dir, name := apkTarget(tool)
	if name == "" {
		return false
	}
	return a.http.Check(ctx, rn, tool, httpDelegate(mc, dir, name))
}

// Install downloads the .apk under the stable "<tool>.apk" name via
// HTTPAdapter, then dispatches it to Android's package installer.
func (a *AndroidAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if err := requireMethodArtifact("android", mc); err != nil {
		return err
	}

	dir, name := apkTarget(tool)
	if name == "" {
		return fmt.Errorf("android: tool has no name to derive the .apk filename from")
	}

	if err := a.http.Install(ctx, rn, tool, httpDelegate(mc, dir, name)); err != nil {
		return fmt.Errorf("android: %w", err)
	}

	apkPath := filepath.Join(dir, name)
	res := rn.Run(ctx, "termux-open", apkPath)
	if err := run.CheckResult(res, "termux-open"); err != nil {
		return fmt.Errorf("android: dispatch %s to package installer: %w", apkPath, err)
	}
	return nil
}

// Remove is deliberately not implemented. depengine only ever handed a
// downloaded .apk to Android's package installer — whether the app is
// actually present depends on a human having tapped through that prompt,
// outside this process, and no am/pm command reliably uninstalls an app
// from Termux without root/adb. Deleting the cached .apk here would not
// uninstall the app and would misleadingly suggest it did. Kept manual,
// same policy as "vscode"/"mas"/"apm" (see pkg/ecosystem/registry.go).

// Compile-time interface check.
var _ exec.Adapter = (*AndroidAdapter)(nil)
