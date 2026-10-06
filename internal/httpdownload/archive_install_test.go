package httpdownload

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exectest"
	"github.com/Khorea1/depengine/internal/run"
)

func TestInstallArchiveElevatesPayloadAndLinkIndependently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX archive and launcher assertion")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can create the system launcher without elevation")
	}
	for _, tc := range []struct {
		name           string
		payloadInHome  bool
		linkInHome     bool
		wantElevatedLn bool
	}{
		{name: "system link with user payload", payloadInHome: true, linkInHome: false, wantElevatedLn: true},
		{name: "user link with system payload", payloadInHome: false, linkInHome: true, wantElevatedLn: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			exectest.SetHome(t, home)
			payloadBase, linkBase := filepath.Join(root, "system"), filepath.Join(root, "system")
			if tc.payloadInHome {
				payloadBase = filepath.Join(home, "tools")
			}
			if tc.linkInHome {
				linkBase = filepath.Join(home, "bin")
			}
			dest, links := filepath.Join(payloadBase, "demo"), linkBase
			archive := filepath.Join(root, "demo.tar.gz")
			writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("binary"))
			mc := &config.MethodCandidate{Config: map[string]any{
				"extract_to":    dest,
				"link_dir":      links,
				"entrypoints":   map[string]any{"demo": "bin/demo"},
				"sudo_required": false,
			}}
			// In the second case, payload elevation is explicitly requested by
			// its system path; the launcher still belongs in the user path.
			if !tc.payloadInHome {
				delete(mc.Config, "sudo_required")
			}
			run.OverrideElevation("sudo")
			defer run.OverrideElevation("")
			rn := &scriptedArchiveRunner{}
			if err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, rn); err != nil {
				t.Fatalf("installArchive() error = %v", err)
			}
			if !tc.payloadInHome {
				info, err := os.Stat(dest)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o755 {
					t.Fatalf("elevated payload root mode = %04o, want 0755", info.Mode().Perm())
				}
				info, err = os.Stat(filepath.Join(dest, "bin", "demo"))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o755 {
					t.Fatalf("elevated payload member mode = %04o, want preserved 0755", info.Mode().Perm())
				}
			}

			gotElevatedLn := false
			for _, call := range rn.calls {
				if call.Name == "sudo" && len(call.Args) >= 1 && call.Args[0] == "ln" {
					gotElevatedLn = true
				}
			}
			if gotElevatedLn != tc.wantElevatedLn {
				t.Fatalf("elevated ln = %v, want %v; calls: %+v", gotElevatedLn, tc.wantElevatedLn, rn.calls)
			}
		})
	}
}

func TestArchiveRemoveElevatesLinkIndependently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission and symlink assertion")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can remove the protected launcher without elevation")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	lockedParent := filepath.Join(root, "system")
	payload := filepath.Join(home, "tools", "demo")
	links := filepath.Join(lockedParent, "bin")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o700); err != nil {
		t.Fatal(err)
	}
	exectest.SetHome(t, home)
	owned := filepath.Join(payload, "bin", "demo")
	if err := os.WriteFile(owned, []byte("owned"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	launcher := filepath.Join(links, "demo")
	if err := os.Symlink(owned, launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(links, 0o500); err != nil { // #nosec G302 -- test intentionally changes fixture permissions to exercise permission behavior.
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(links, 0o700); err != nil { // #nosec G302 -- cleanup restores fixture permissions before TempDir removal.
			t.Errorf("restore launcher directory permissions: %v", err)
		}
	})

	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	fr := &run.FakeRunner{}
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to":           payload,
		"link_dir":             links,
		"entrypoints":          map[string]any{"demo": "bin/demo"},
		ownedArchivePayloadKey: true,
	}}
	owner, err := expectedArchiveOwnership(&config.Tool{Name: "demo"}, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveOwnership(payload, owner); err != nil {
		t.Fatal(err)
	}
	if err := NewHTTPAdapter().Remove(context.Background(), fr, &config.Tool{Name: "demo"}, mc); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(fr.Calls) != 1 || fr.Calls[0].Name != "sudo" || len(fr.Calls[0].Args) < 4 || fr.Calls[0].Args[0] != "rm" || fr.Calls[0].Args[1] != "-f" || fr.Calls[0].Args[3] != launcher {
		t.Fatalf("elevated launcher removal calls = %+v, want sudo rm -f %s", fr.Calls, launcher)
	}
	if _, err := os.Stat(payload); !os.IsNotExist(err) {
		t.Fatalf("user payload remains: %v", err)
	}
}

func TestInstallArchiveStripEntrypointCheckRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher assertion")
	}
	for _, format := range []string{"tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			exectest.SetHome(t, home)
			archive := filepath.Join(root, "nvim."+format)
			writeTestArchive(t, archive, format, "nvim-linux/bin/nvim", []byte("binary"))
			dest, links := filepath.Join(home, "opt", "nvim"), filepath.Join(home, "bin")
			mc := &config.MethodCandidate{Config: map[string]any{"extract_to": dest, "strip_components": int64(1), "entrypoints": map[string]any{"nvim": "bin/nvim"}, "link_dir": links}}
			mc.Config["sudo_required"] = false
			tool := &config.Tool{Name: "nvim"}
			ext := "." + format
			if err := installArchive(context.Background(), archive, ext, tool, mc, run.OSExecRunner{}); err != nil {
				t.Fatal(err)
			}
			if format == "tar.gz" {
				info, err := os.Stat(filepath.Join(dest, "bin", "nvim"))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o755 {
					t.Fatalf("installed executable mode = %v, want 0755", info.Mode().Perm())
				}
			}
			mc.Config[ownedArchivePayloadKey] = true // Direct installArchive invocation bypasses adapter ownership persistence.
			adapter := NewHTTPAdapter()
			if !adapter.Check(context.Background(), run.OSExecRunner{}, tool, mc) {
				t.Fatal("check failed after install")
			}
			if err := adapter.Remove(context.Background(), run.OSExecRunner{}, tool, mc); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatalf("payload remains: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(links, "nvim")); !os.IsNotExist(err) {
				t.Fatalf("launcher remains: %v", err)
			}
		})
	}
}

func writeTestArchive(t *testing.T, path, format, name string, body []byte) {
	t.Helper()
	f, err := os.Create(path) // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if format == "zip" {
		w := zip.NewWriter(f)
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRejectsInstallerArtifacts(t *testing.T) {
	tool := &config.Tool{Name: "nvim"}
	mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.invalid/nvim.msi"}}
	err := NewHTTPAdapter().Install(context.Background(), &run.FakeRunner{}, tool, mc)
	if err == nil {
		t.Fatal("expected installer rejection")
	}
}

func TestArchiveEntrypointRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("x"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	if err := requirePayloadFile(payload, "../outside"); err == nil {
		t.Fatal("expected traversal entrypoint to be rejected")
	}
	if err := requirePayloadFile(payload, outside); err == nil {
		t.Fatal("expected absolute entrypoint to be rejected")
	}
}

func TestCopyTreeStrippedRejectsSourceSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside", filepath.Join(src, "top", "link")); err != nil {
		t.Fatal(err)
	}

	err := copyTreeStripped(src, dest, 0)
	if err == nil || !strings.Contains(err.Error(), "escapes staging") {
		t.Fatalf("copyTreeStripped() error = %v, want staging escape rejection", err)
	}
}

func TestCopyTreeStrippedRejectsSymlinkEscapeIntroducedByStrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top", "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "outside"), []byte("inside staging"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Safe before stripping: top/a/../../outside resolves inside src.
	// After stripping top/, a/../../outside would escape the payload root.
	if err := os.Symlink("../../outside", filepath.Join(src, "top", "a", "link")); err != nil {
		t.Fatal(err)
	}

	err := copyTreeStripped(src, dest, 1)
	if err == nil || !strings.Contains(err.Error(), "escapes stripped payload") {
		t.Fatalf("copyTreeStripped() error = %v, want stripped-payload escape rejection", err)
	}
}

func TestCopyTreeStrippedPreservesContainedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(filepath.Join(src, "top", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top", "bin", "demo"), []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("demo", filepath.Join(src, "top", "bin", "current")); err != nil {
		t.Fatal(err)
	}

	if err := copyTreeStripped(src, dest, 1); err != nil {
		t.Fatalf("copyTreeStripped() error = %v", err)
	}
	payloadRoot, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = payloadRoot.Close() }()
	link, err := payloadRoot.Readlink(filepath.Join("bin", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "demo" {
		t.Fatalf("copied symlink target = %q, want demo", link)
	}
	data, err := payloadRoot.ReadFile(filepath.Join("bin", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary" {
		t.Fatalf("copied file = %q, want binary", string(data))
	}
}

func TestArchiveRemoveRefusesRedirectedLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink assertion")
	}
	root := t.TempDir()
	payload := filepath.Join(root, "opt", "nvim")
	links := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(links, 0o700); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(payload, "bin", "nvim")
	if err := os.WriteFile(owned, []byte("owned"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o700); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	launcher := filepath.Join(links, "nvim")
	if err := os.Symlink(foreign, launcher); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to":  payload,
		"entrypoints": map[string]any{"nvim": "bin/nvim"},
		"link_dir":    links,
	}}
	err := NewHTTPAdapter().Remove(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "nvim"}, mc)
	if err == nil {
		t.Fatal("expected redirected launcher removal to be refused")
	}
	if _, statErr := os.Stat(payload); statErr != nil {
		t.Fatalf("owned payload was removed after refusal: %v", statErr)
	}
	actual, readErr := os.Readlink(launcher)
	if readErr != nil || actual != foreign {
		t.Fatalf("foreign launcher changed: target=%q err=%v", actual, readErr)
	}
}

func TestHTTPRejectsAllPlatformInstallerArtifacts(t *testing.T) {
	for _, ext := range []string{".msi", ".exe", ".pkg", ".dmg", ".msix", ".appx"} {
		t.Run(ext, func(t *testing.T) {
			mc := &config.MethodCandidate{Config: map[string]any{"url": "https://example.invalid/tool" + ext}}
			err := NewHTTPAdapter().Install(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "tool"}, mc)
			if err == nil {
				t.Fatalf("expected %s rejection", ext)
			}
		})
	}
}

type scriptedArchiveRunner struct {
	calls []run.FakeCall
	fail  map[string]int
	skip  map[string]int
}

func (r *scriptedArchiveRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	command, commandArgs := name, args
	if name == "sudo" {
		if len(args) == 0 {
			return run.Result{ExitCode: 1, Stderr: []byte("missing elevated command")}
		}
		command, commandArgs = args[0], args[1:]
	}
	if r.skip[command] > 0 {
		r.skip[command]--
	} else if r.fail[command] > 0 {
		r.fail[command]--
		return run.Result{ExitCode: 1, Stderr: []byte("injected " + command + " failure")}
	}
	if command == "chown" {
		// Keep ownership normalization mockable without requiring host root.
		return run.Result{}
	}
	return (run.OSExecRunner{}).Run(ctx, command, commandArgs...)
}

func TestCommitElevatedPayloadNormalizesOwnershipRecursivelyAndPreservesModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX elevated payload metadata")
	}
	ctx := context.Background()
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	stage := filepath.Join(root, "stage")
	if err := os.MkdirAll(filepath.Join(stage, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "bin", "demo"), []byte("payload"), 0o751); err != nil { // #nosec G306 -- fixture preserves the archive executable mode under t.TempDir().
		t.Fatal(err)
	}
	rn := &scriptedArchiveRunner{}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	if err := commitPayload(ctx, rn, stage, dest, dest+".depengine-backup", true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("payload root mode = %04o, want 0755", info.Mode().Perm())
	}
	info, err = os.Stat(filepath.Join(dest, "bin", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("payload member mode = %04o, want preserved 0751", info.Mode().Perm())
	}
	var commands []string
	for _, call := range rn.calls {
		for _, arg := range call.Args {
			if arg == "mv" || arg == "chown" || arg == "chmod" {
				commands = append(commands, arg)
				break
			}
		}
	}
	want := []string{"mv", "chown", "chmod"}
	if strings.Join(commands, ",") != strings.Join(want, ",") {
		t.Fatalf("commit commands = %v, want %v", commands, want)
	}
	var ownerCommand, modeCommand string
	for _, call := range rn.calls {
		joined := strings.Join(call.Args, " ")
		if strings.Contains(joined, "chown") {
			ownerCommand = joined
		}
		if strings.Contains(joined, "chmod") {
			modeCommand = joined
		}
	}
	if !strings.HasPrefix(ownerCommand, "chown -h 0 "+dest) || !strings.Contains(ownerCommand, filepath.Join(dest, "bin", "demo")) || strings.Contains(ownerCommand, " -R ") || strings.Contains(ownerCommand, "0:0") {
		t.Fatalf("owner normalization command = %q", ownerCommand)
	}
	if modeCommand != "chmod 0755 "+dest {
		t.Fatalf("mode normalization command = %q", modeCommand)
	}
}

func TestCommitElevatedPayloadOwnershipDoesNotDereferenceSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink ownership semantics")
	}
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	dest := filepath.Join(root, "payload")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stage, "link")); err != nil {
		t.Fatal(err)
	}
	rn := &scriptedArchiveRunner{}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	if err := commitPayload(context.Background(), rn, stage, dest, dest+".depengine-backup", true); err != nil {
		t.Fatal(err)
	}
	var ownerCommand string
	for _, call := range rn.calls {
		if len(call.Args) > 0 && call.Args[0] == "chown" {
			ownerCommand = strings.Join(call.Args, " ")
		}
	}
	if !strings.Contains(ownerCommand, "chown -h 0") || !strings.Contains(ownerCommand, filepath.Join(dest, "link")) {
		t.Fatalf("owner normalization command = %q, want -h and payload symlink", ownerCommand)
	}
	if strings.Contains(ownerCommand, outside) {
		t.Fatalf("owner normalization escaped payload through symlink target: %q", ownerCommand)
	}
}

func TestRollbackPayloadReportsElevatedRemovalAndRestoreFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX elevated rollback")
	}
	root := t.TempDir()
	dest, backup := filepath.Join(root, "payload"), filepath.Join(root, "payload.depengine-backup")
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	rn := &scriptedArchiveRunner{fail: map[string]int{"rm": 1, "mv": 1}}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	err := rollbackPayload(context.Background(), rn, dest, backup, true)
	if err == nil || !strings.Contains(err.Error(), "remove owned path") || !strings.Contains(err.Error(), "restore payload") {
		t.Fatalf("rollback error = %v, want removal and restore failures", err)
	}
}

func TestRollbackPayloadReportsNonElevatedRemovalAndRestoreFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission failure injection")
	}
	if runAsUnprivilegedTest(t) {
		return
	}
	root := t.TempDir()
	dest, backup := filepath.Join(root, "payload"), filepath.Join(root, "payload.depengine-backup")
	locked := filepath.Join(dest, "locked")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "keep"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil { // #nosec G302 -- locked fixture deliberately denies owner writes to test rollback behavior.
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(locked, 0o700); err != nil { // #nosec G302 -- restore owner write permission only to clean up the deliberately locked test fixture.
			t.Errorf("restore locked fixture permissions: %v", err)
		}
	}()
	err := rollbackPayload(context.Background(), &scriptedArchiveRunner{}, dest, backup, false)
	if err == nil || !strings.Contains(err.Error(), "remove owned path") || !strings.Contains(err.Error(), "restore payload") {
		t.Fatalf("rollback error = %v, want removal and restore failures", err)
	}
}

func TestInstallArchiveKeepsCommittedInstallOnNonElevatedBackupCleanupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX backup cleanup failure injection")
	}
	if runAsUnprivilegedTest(t) {
		return
	}
	root := t.TempDir()
	dest := filepath.Join(root, "payload")
	locked := filepath.Join(dest, "locked")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "old"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	mc := &config.MethodCandidate{Config: map[string]any{"extract_to": dest, "sudo_required": false}}
	owner, err := expectedArchiveOwnership(&config.Tool{Name: "demo"}, mc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveOwnership(dest, owner); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil { // #nosec G302 -- locked fixture deliberately denies owner writes to test replacement cleanup.
		t.Fatal(err)
	}
	backupLocked := filepath.Join(dest+".depengine-backup", "locked")
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o700)       // #nosec G302 -- cleanup restores test-owned locked fixture permissions.
		_ = os.Chmod(backupLocked, 0o700) // #nosec G302 -- cleanup restores test-owned locked fixture permissions.
	})
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	err = installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, &scriptedArchiveRunner{})
	if err != nil {
		t.Fatalf("post-commit backup cleanup must not fail installation: %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(dest, "bin", "demo")); readErr != nil || string(got) != "new" { // #nosec G304 -- dest is a test-owned temporary path.
		t.Fatalf("committed payload = %q, %v; want new payload", got, readErr)
	}
}

func TestInstallArchiveKeepsCommittedInstallOnElevatedBackupCleanupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX elevated backup cleanup failure")
	}
	if runAsUnprivilegedTest(t) {
		return
	}
	root := t.TempDir()
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	dest := filepath.Join(root, "payload")
	rn := &scriptedArchiveRunner{fail: map[string]int{"rm": 1}, skip: map[string]int{"rm": 1}}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	mc := &config.MethodCandidate{Config: map[string]any{"extract_to": dest, "sudo_required": true}}
	err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, rn)
	if err != nil {
		t.Fatalf("post-commit elevated backup cleanup must not fail installation: %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(dest, "bin", "demo")); readErr != nil || string(got) != "new" { // #nosec G304 -- dest is a test-owned temporary path.
		t.Fatalf("committed payload = %q, %v; want new payload", got, readErr)
	}
}

func TestInstallArchiveJoinsElevatedLauncherRollbackAndCleanupFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX elevated launcher transaction")
	}
	if runAsUnprivilegedTest(t) {
		return
	}
	root := t.TempDir()
	archive := filepath.Join(root, "demo.tar.gz")
	writeTestArchive(t, archive, "tar.gz", "bin/demo", []byte("new"))
	dest := filepath.Join(root, "payload")
	rn := &scriptedArchiveRunner{fail: map[string]int{"ln": 1, "rm": 10}, skip: map[string]int{"ln": 1, "rm": 1}}
	run.OverrideElevation("sudo")
	defer run.OverrideElevation("")
	mc := &config.MethodCandidate{Config: map[string]any{
		"extract_to": dest, "link_dir": filepath.Join(root, "bin"), "sudo_required": true,
		"entrypoints": map[string]any{"demo": "bin/demo", "demo2": "bin/demo"},
	}}
	err := installArchive(context.Background(), archive, ".tar.gz", &config.Tool{Name: "demo"}, mc, rn)
	if err == nil || !strings.Contains(err.Error(), "create symlink") || !strings.Contains(err.Error(), "remove owned path") {
		t.Fatalf("install error = %v, want launcher and rollback/cleanup failures", err)
	}
}
