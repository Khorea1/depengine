package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/run"
)

type detectorRunner struct {
	commands map[string]string
	paths    map[string]bool
}

func (r *detectorRunner) ExecutionAllowed() bool { return true }

func (r *detectorRunner) LookPath(_ context.Context, name string) bool {
	return r.paths[name]
}

func (r *detectorRunner) Run(_ context.Context, name string, args ...string) run.Result {
	key := strings.Join(append([]string{name}, args...), " ")
	out, ok := r.commands[key]
	if !ok {
		return run.Result{ExitCode: 127}
	}
	return run.Result{Stdout: []byte(out)}
}

func fixtureDetector(files map[string]string, env map[string]string, commands map[string]string, paths map[string]bool) hostDetector {
	return hostDetector{
		runner: &detectorRunner{commands: commands, paths: paths},
		goos:   "linux",
		goarch: "amd64",
		getenv: func(key string) string { return env[key] },
		readFile: func(path string) ([]byte, error) {
			if value, ok := files[path]; ok {
				return []byte(value), nil
			}
			return nil, errors.New("not found")
		},
		exists: func(path string) bool {
			_, ok := files[path]
			return ok
		},
		isDir:        func(path string) bool { return files[path] == "<dir>" },
		isExecutable: func(path string) bool { return files[path] == "<exe>" },
		readlink: func(path string) (string, error) {
			if value, ok := files[path]; ok && strings.HasPrefix(value, "->") {
				return strings.TrimPrefix(value, "->"), nil
			}
			return "", errors.New("not a link")
		},
	}
}

func baseCommands() map[string]string {
	return map[string]string{
		"uname -s": "Linux\n",
		"uname -r": "6.12.0\n",
		"uname -m": "x86_64\n",
	}
}

func TestDetectorLinuxOSReleaseAndRuntimeTraits(t *testing.T) {
	commands := baseCommands()
	commands["ldd --version"] = "ldd (GNU C Library) 2.40\n"
	d := fixtureDetector(
		map[string]string{
			"/etc/os-release":     "ID=ubuntu\nNAME=Ubuntu\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\nID_LIKE=debian\nVERSION_ID=24.04\n",
			"/proc/version":       "Linux version 6.12 Microsoft WSL2",
			"/.dockerenv":         "",
			"/run/systemd/system": "<dir>",
		},
		map[string]string{}, commands, map[string]bool{"ldd": true},
	)
	facts := d.detect(context.Background())
	if facts.DistroID != "ubuntu" || facts.DistroVersion != "24.04" || facts.DistroIDLike != "debian" {
		t.Fatalf("unexpected distro facts: %#v", facts)
	}
	if facts.TargetArch != "x86_64" || facts.OS != "linux" || facts.Libc != "glibc" || facts.InitSystem != "systemd" {
		t.Fatalf("unexpected runtime facts: %#v", facts)
	}
	if !facts.IsWSL || !facts.IsContainer {
		t.Fatalf("environment flags not detected: %#v", facts)
	}
}

func TestDetectorTermuxAndProotPrecedence(t *testing.T) {
	t.Run("bare termux", func(t *testing.T) {
		d := fixtureDetector(nil, map[string]string{"TERMUX_VERSION": "0.119"}, baseCommands(), nil)
		facts := d.detect(context.Background())
		if facts.DistroID != "termux" || !facts.IsAndroid || facts.DetectionMethod != "termux" {
			t.Fatalf("facts = %#v", facts)
		}
	})

	t.Run("proot guest wins", func(t *testing.T) {
		d := fixtureDetector(
			map[string]string{"/etc/os-release": "ID=debian\nNAME=Debian\nVERSION_ID=13\n"},
			map[string]string{"TERMUX_VERSION": "0.119", "PREFIX": "/data/data/com.termux/files/usr"},
			baseCommands(), nil,
		)
		facts := d.detect(context.Background())
		if facts.DistroID != "debian" || facts.IsAndroid || facts.DetectionMethod != "os-release" {
			t.Fatalf("facts = %#v", facts)
		}
	})
}

func TestDetectorAndroidAndProotPrecedence(t *testing.T) {
	t.Run("android", func(t *testing.T) {
		commands := baseCommands()
		commands["getprop ro.build.version.release"] = "16\n"
		d := fixtureDetector(map[string]string{"/system/build.prop": "present"}, nil, commands, map[string]bool{"getprop": true})
		facts := d.detect(context.Background())
		if facts.DistroID != "android" || facts.DistroVersion != "16" || !facts.IsAndroid {
			t.Fatalf("facts = %#v", facts)
		}
	})

	t.Run("linux guest wins", func(t *testing.T) {
		d := fixtureDetector(
			map[string]string{"/system/build.prop": "present", "/etc/os-release": "ID=arch\nNAME=Arch Linux\n"},
			nil, baseCommands(), map[string]bool{"getprop": true},
		)
		facts := d.detect(context.Background())
		if facts.DistroID != "arch" || facts.IsAndroid {
			t.Fatalf("facts = %#v", facts)
		}
	})
}

func TestDetectorDarwinBSDAndWindows(t *testing.T) {
	t.Run("darwin", func(t *testing.T) {
		commands := map[string]string{
			"uname -s": "Darwin", "uname -r": "25.0.0", "uname -m": "arm64",
			"sw_vers -productName": "macOS", "sw_vers -productVersion": "16.0",
		}
		d := fixtureDetector(nil, nil, commands, nil)
		d.goos, d.goarch = "darwin", "arm64"
		facts := d.detect(context.Background())
		if facts.DistroID != "macos" || facts.DistroVersion != "16.0" || facts.TargetArch != "arm64" {
			t.Fatalf("facts = %#v", facts)
		}
	})

	t.Run("freebsd", func(t *testing.T) {
		commands := map[string]string{"uname -s": "FreeBSD", "uname -r": "15.0", "uname -m": "amd64"}
		d := fixtureDetector(nil, nil, commands, nil)
		d.goos = "freebsd"
		facts := d.detect(context.Background())
		if facts.DistroID != "freebsd" || facts.DetectionMethod != "bsd" {
			t.Fatalf("facts = %#v", facts)
		}
	})

	t.Run("native windows", func(t *testing.T) {
		commands := map[string]string{"cmd.exe /d /c ver": "Microsoft Windows [Version 10.0.26100.4652]"}
		d := fixtureDetector(nil, nil, commands, nil)
		d.goos, d.goarch = "windows", "amd64"
		facts := d.detect(context.Background())
		if facts.DistroID != "windows" || facts.DistroVersion != "10.0.26100.4652" || facts.DetectionMethod != "go-builtin+cmd-ver" {
			t.Fatalf("facts = %#v", facts)
		}
	})
}


func TestDetectorCoversEveryKnownNativeClan(t *testing.T) {
	type fixture struct {
		files    map[string]string
		env      map[string]string
		commands map[string]string
		paths    map[string]bool
		goos     string
		goarch   string
	}

	osRelease := func(id string) map[string]string {
		return map[string]string{"/etc/os-release": "ID=" + id + "\nNAME=" + id + "\n"}
	}

	cases := map[string]fixture{
		"debian": {files: osRelease("ubuntu")},
		"arch":   {files: osRelease("arch")},
		"fedora": {files: osRelease("fedora")},
		"suse":   {files: osRelease("opensuse-tumbleweed")},
		"alpine": {files: osRelease("alpine")},
		"void":   {files: osRelease("void")},
		"gentoo": {files: osRelease("gentoo")},
		"mint":   {files: osRelease("linuxmint")},
		"opkg":   {files: osRelease("openwrt")},
		"termux": {env: map[string]string{"TERMUX_VERSION": "0.119"}},
		"macos": {
			commands: map[string]string{
				"uname -s": "Darwin",
				"uname -r": "25.0.0",
				"uname -m": "arm64",
			},
			goos: "darwin", goarch: "arm64",
		},
		"freebsd": {
			commands: map[string]string{"uname -s": "FreeBSD", "uname -r": "15.0", "uname -m": "amd64"},
			goos: "freebsd",
		},
		"openbsd": {
			commands: map[string]string{"uname -s": "OpenBSD", "uname -r": "7.8", "uname -m": "amd64"},
			goos: "openbsd",
		},
		"netbsd": {
			commands: map[string]string{"uname -s": "NetBSD", "uname -r": "10.1", "uname -m": "amd64"},
			goos: "netbsd",
		},
		"windows": {
			commands: map[string]string{"cmd.exe /d /c ver": "Microsoft Windows [Version 10.0.26100.4652]"},
			goos: "windows", goarch: "amd64",
		},
	}

	for _, clan := range native.KnownClans() {
		tc, ok := cases[clan]
		if !ok {
			t.Fatalf("native clan %q has no host-detection fixture", clan)
		}
		t.Run(clan, func(t *testing.T) {
			commands := tc.commands
			if commands == nil {
				commands = baseCommands()
			}
			d := fixtureDetector(tc.files, tc.env, commands, tc.paths)
			if tc.goos != "" {
				d.goos = tc.goos
			}
			if tc.goarch != "" {
				d.goarch = tc.goarch
			}
			facts := d.detect(context.Background())
			if got := ResolveFamily(facts); got != clan {
				t.Fatalf("ResolveFamily(%#v) = %q, want %q", facts, got, clan)
			}
		})
	}
}
