package platform

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/run"
)

const detectionCommandTimeout = 2 * time.Second

// Detect gathers host facts without a shell orchestration layer. Filesystem and
// environment signals are inspected directly; the few command probes that are
// still useful cross the shared run.Runner subprocess boundary.
func Detect(ctx context.Context, rn run.Runner) *Facts {
	d := newHostDetector(rn)
	return d.detect(ctx)
}

type hostDetector struct {
	runner       run.Runner
	goos         string
	goarch       string
	getenv       func(string) string
	readFile     func(string) ([]byte, error)
	exists       func(string) bool
	isDir        func(string) bool
	isExecutable func(string) bool
	readlink     func(string) (string, error)
}

func newHostDetector(rn run.Runner) hostDetector {
	return hostDetector{
		runner:       rn,
		goos:         runtime.GOOS,
		goarch:       runtime.GOARCH,
		getenv:       os.Getenv,
		readFile:     os.ReadFile,
		exists:       pathExists,
		isDir:        pathIsDir,
		isExecutable: pathIsExecutable,
		readlink:     os.Readlink,
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathIsExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func (d hostDetector) detect(ctx context.Context) *Facts {
	kernelName := d.commandText(ctx, "uname", "-s")
	if kernelName == "" {
		kernelName = runtimeKernelName(d.goos)
	}
	kernelVersion := d.commandText(ctx, "uname", "-r")
	if kernelVersion == "" {
		kernelVersion = "unknown"
	}
	arch := d.commandText(ctx, "uname", "-m")
	if arch == "" {
		arch = normalizeRuntimeArch(d.goarch)
	}

	facts := &Facts{
		TargetArch:   arch,
		TargetFamily: "unknown",
		Confidence:   "low",
		Kernel:       kernelVersion,
		Libc:         "unknown",
		InitSystem:   "unknown",
		OS:           strings.ToLower(kernelName),
	}
	if facts.OS == "" {
		facts.OS = "unknown"
	}

	d.detectEnvironmentFlags(facts)

	if d.goos == "windows" && !isPOSIXWindowsKernel(kernelName) {
		d.detectNativeWindows(ctx, facts)
		d.detectRuntimeTraits(ctx, facts)
		return facts
	}

	release, hasRelease := d.readOSRelease()
	realGuest := hasRelease && release.ID != "" && release.ID != "termux" && release.ID != "android"

	switch {
	case d.isTermux() && !realGuest:
		facts.DistroID = "termux"
		facts.DistroName = "Termux"
		facts.DistroVersion = d.getenv("TERMUX_VERSION")
		facts.TargetFamily = "unix"
		facts.DetectionMethod = "termux"
		facts.IsAndroid = true
		facts.Confidence = "high"
	case d.isAndroid(ctx) && !realGuest:
		facts.DistroID = "android"
		facts.DistroName = "Android"
		if version := d.commandText(ctx, "getprop", "ro.build.version.release"); version != "" {
			facts.DistroVersion = version
			facts.DistroName += " " + version
		}
		facts.TargetFamily = "unix"
		facts.DetectionMethod = "android-generic"
		facts.IsAndroid = true
		facts.Confidence = "medium"
	case hasRelease && (release.ID != "" || release.Name != ""):
		d.applyOSRelease(facts, release)
	case kernelName == "Darwin":
		d.detectDarwin(ctx, facts)
	case isBSDKernel(kernelName):
		d.detectBSD(facts, kernelName, kernelVersion)
	case isPOSIXWindowsKernel(kernelName):
		facts.DistroID = "windows"
		facts.DistroName = "Windows (via " + kernelName + ")"
		if kernelVersion != "unknown" {
			facts.DistroVersion = kernelVersion
		}
		facts.TargetFamily = "windows"
		facts.DetectionMethod = "windows-posix-layer"
		facts.Confidence = "high"
	case kernelName != "":
		facts.DistroID = "unknown"
		facts.DistroName = kernelName
		if kernelVersion != "unknown" {
			facts.DistroName += " " + kernelVersion
		}
		facts.TargetFamily = "unix"
		facts.DetectionMethod = "uname-fallback"
		facts.Confidence = "low"
	default:
		facts.DistroID = "unknown"
		facts.DistroName = "unknown"
		facts.DetectionMethod = "failed-noninteractive"
		facts.Confidence = "none"
	}

	d.detectRuntimeTraits(ctx, facts)
	return facts
}

func runtimeKernelName(goos string) string {
	switch goos {
	case "darwin":
		return "Darwin"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	case "dragonfly":
		return "DragonFly"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func (d hostDetector) detectEnvironmentFlags(facts *Facts) {
	facts.IsContainer = d.exists("/.dockerenv") || d.exists("/run/.containerenv") || d.getenv("container") != ""
	if !facts.IsContainer {
		for _, path := range []string{"/proc/1/cgroup", "/proc/self/cgroup"} {
			if data, err := d.readFile(path); err == nil {
				text := strings.ToLower(string(data))
				if strings.Contains(text, "docker") || strings.Contains(text, "containerd") || strings.Contains(text, "lxc") {
					facts.IsContainer = true
					break
				}
			}
		}
	}
	if data, err := d.readFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft") {
		facts.IsWSL = true
	}
	if d.getenv("WSL_DISTRO_NAME") != "" || d.getenv("WSL_INTEROP") != "" {
		facts.IsWSL = true
	}
}

func (d hostDetector) isTermux() bool {
	return d.getenv("TERMUX_VERSION") != "" || strings.Contains(d.getenv("PREFIX"), "com.termux")
}

func (d hostDetector) isAndroid(ctx context.Context) bool {
	return d.lookPath(ctx, "getprop") || d.exists("/system/build.prop")
}

func (d hostDetector) readOSRelease() (osRelease, bool) {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := d.readFile(path)
		if err == nil {
			return parseOSRelease(data), true
		}
	}
	return osRelease{}, false
}

func (d hostDetector) applyOSRelease(facts *Facts, release osRelease) {
	if release.ID != "" {
		facts.DistroID = strings.ToLower(release.ID)
	} else {
		facts.DistroID = strings.ReplaceAll(strings.ToLower(release.Name), " ", "-")
	}
	switch {
	case release.PrettyName != "":
		facts.DistroName = release.PrettyName
	case release.Name != "":
		facts.DistroName = release.Name
	default:
		facts.DistroName = facts.DistroID
	}
	facts.DistroVersion = release.VersionID
	facts.DistroIDLike = release.IDLike
	facts.TargetFamily = "unix"
	facts.DetectionMethod = "os-release"
	facts.Confidence = "high"
}

func (d hostDetector) detectDarwin(ctx context.Context, facts *Facts) {
	name := d.commandText(ctx, "sw_vers", "-productName")
	version := d.commandText(ctx, "sw_vers", "-productVersion")
	facts.DistroID = "macos"
	facts.DistroName = "macOS"
	if name != "" {
		facts.DistroName = name
	}
	if version != "" {
		facts.DistroVersion = version
		facts.DistroName += " " + version
	}
	facts.TargetFamily = "unix"
	facts.DetectionMethod = "macos"
	facts.Confidence = "high"
}

func (d hostDetector) detectBSD(facts *Facts, kernelName, kernelVersion string) {
	facts.DistroID = strings.ToLower(kernelName)
	facts.DistroName = kernelName
	if kernelVersion != "unknown" {
		facts.DistroVersion = kernelVersion
		facts.DistroName += " " + kernelVersion
	}
	facts.TargetFamily = "unix"
	facts.DetectionMethod = "bsd"
	facts.Confidence = "high"
}

func (d hostDetector) detectNativeWindows(ctx context.Context, facts *Facts) {
	facts.OS = "windows"
	facts.DistroID = "windows"
	facts.DistroName = "Windows"
	facts.TargetFamily = "windows"
	facts.DetectionMethod = "go-builtin"
	facts.Confidence = "low"
	if version := parseWindowsVersion(d.commandText(ctx, "cmd.exe", "/d", "/c", "ver")); version != "" {
		facts.DistroVersion = version
		facts.DistroName += " " + version
		facts.DetectionMethod = "go-builtin+cmd-ver"
		facts.Confidence = "medium"
	}
}

func (d hostDetector) detectRuntimeTraits(ctx context.Context, facts *Facts) {
	if d.lookPath(ctx, "ldd") {
		firstLine, _, _ := strings.Cut(d.commandText(ctx, "ldd", "--version"), "\n")
		switch {
		case strings.Contains(firstLine, "musl"):
			facts.Libc = "musl"
		case strings.Contains(firstLine, "GLIBC"), strings.Contains(firstLine, "glibc"), strings.Contains(firstLine, "GNU C Library"):
			facts.Libc = "glibc"
		case strings.Contains(firstLine, "uClibc"):
			facts.Libc = "uClibc"
		case strings.Contains(firstLine, "FreeBSD"):
			facts.Libc = "freebsd-libc"
		}
	}
	if facts.Libc == "unknown" && d.lookPath(ctx, "getconf") && d.commandText(ctx, "getconf", "GNU_LIBC_VERSION") != "" {
		facts.Libc = "glibc"
	}

	initLink, _ := d.readlink("/sbin/init")
	switch {
	case d.isDir("/run/systemd/system") || initLink == "/usr/lib/systemd/systemd":
		facts.InitSystem = "systemd"
	case d.isExecutable("/sbin/openrc-run") || d.isDir("/etc/openrc"):
		facts.InitSystem = "openrc"
	case d.lookPath(ctx, "runit"):
		facts.InitSystem = "runit"
	case d.isExecutable("/sbin/init") && initLink != "":
		lower := strings.ToLower(initLink)
		switch {
		case strings.Contains(lower, "systemd"):
			facts.InitSystem = "systemd"
		case strings.Contains(lower, "openrc"):
			facts.InitSystem = "openrc"
		case strings.Contains(lower, "runit"):
			facts.InitSystem = "runit"
		case strings.Contains(lower, "sysvinit"):
			facts.InitSystem = "sysvinit"
		}
	}
}

func (d hostDetector) commandText(ctx context.Context, name string, args ...string) string {
	if !run.ExecutionAllowed(d.runner) {
		return ""
	}
	cmdCtx, cancel := context.WithTimeout(ctx, detectionCommandTimeout)
	defer cancel()
	res := d.runner.Run(cmdCtx, name, args...)
	if res.Err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

func (d hostDetector) lookPath(ctx context.Context, name string) bool {
	return run.ExecutionAllowed(d.runner) && run.LookPath(ctx, d.runner, name)
}

func isBSDKernel(kernel string) bool {
	switch kernel {
	case "FreeBSD", "OpenBSD", "NetBSD", "DragonFly":
		return true
	default:
		return false
	}
}

func isPOSIXWindowsKernel(kernel string) bool {
	return strings.Contains(kernel, "MINGW") || strings.Contains(kernel, "CYGWIN") || strings.Contains(kernel, "MSYS")
}

func parseWindowsVersion(output string) string {
	for i := 0; i < len(output); i++ {
		if output[i] < '0' || output[i] > '9' {
			continue
		}
		start := i
		dots := 0
		for i < len(output) {
			c := output[i]
			if c >= '0' && c <= '9' {
				i++
				continue
			}
			if c == '.' {
				dots++
				i++
				continue
			}
			break
		}
		candidate := strings.TrimSuffix(output[start:i], ".")
		if dots >= 2 && validWindowsVersion(candidate) {
			return candidate
		}
	}
	return ""
}

func validWindowsVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}
