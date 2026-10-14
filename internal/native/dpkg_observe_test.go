package native

import (
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestDpkgPackageIdentityValidation(t *testing.T) {
	for _, invalid := range []string{"", "lib*", "a", "libc6:amd64:extra", "Upper", "--help", "libc6:amd64+"} {
		if BuildDpkgObserveCmd(invalid) != nil {
			t.Errorf("BuildDpkgObserveCmd(%q) must reject unsafe package identity", invalid)
		}
	}
	if BuildDpkgObserveCmd("3proxy") == nil {
		t.Fatal("valid package names beginning with a digit must be accepted")
	}
}

func TestObserveDpkgResults(t *testing.T) {
	const sentinel = "dpkg\tinstall ok installed\n"
	cases := []struct {
		name, pkg, targetOut, sentinelOut string
		targetExit, sentinelExit          int
		want                              DpkgPresence
	}{
		{name: "installed", pkg: "demo", targetOut: "demo\tinstall ok installed\n", sentinelOut: sentinel, want: DpkgPresent},
		{name: "held installed", pkg: "demo", targetOut: "demo\thold ok installed\n", sentinelOut: "dpkg\thold ok installed\n", want: DpkgPresent},
		{name: "deinstall desired but installed", pkg: "demo", targetOut: "demo\tdeinstall ok installed\n", sentinelOut: sentinel, want: DpkgPresent},
		{name: "purge desired but installed", pkg: "demo", targetOut: "demo\tpurge ok installed\n", sentinelOut: sentinel, want: DpkgPresent},
		{name: "residual config files", pkg: "demo", targetOut: "demo\tdeinstall ok config-files\n", sentinelOut: sentinel, want: DpkgAbsent},
		{name: "not installed", pkg: "demo", targetOut: "demo\tdeinstall ok not-installed\n", sentinelOut: sentinel, want: DpkgAbsent},
		{name: "missing target with healthy sentinel", pkg: "missing", targetExit: 1, sentinelOut: sentinel, want: DpkgAbsent},
		{name: "empty database", pkg: "missing", targetExit: 1, sentinelExit: 1, want: DpkgUnknown},
		{name: "empty successful target query", pkg: "missing", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "target error despite healthy sentinel", pkg: "demo", targetExit: 2, sentinelOut: sentinel, want: DpkgUnknown},
		{name: "target failure with output", pkg: "demo", targetExit: 1, targetOut: "demo\tinstall ok installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "reinstreq", pkg: "demo", targetOut: "demo\tinstall reinstreq installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "unknown desired state", pkg: "demo", targetOut: "demo\tother ok installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "unknown error flag", pkg: "demo", targetOut: "demo\tinstall other installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "unknown actual state", pkg: "demo", targetOut: "demo\tinstall ok half-installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "missing sentinel", pkg: "demo", targetOut: "demo\tinstall ok installed\n", want: DpkgUnknown},
		{name: "malformed target", pkg: "demo", targetOut: "demo\tbroken\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "malformed sentinel identity", pkg: "demo", targetOut: "demo\tinstall ok installed\n", sentinelOut: "dpkg:garbage\tinstall ok installed\n", want: DpkgUnknown},
		{name: "multiple unqualified architectures", pkg: "libdemo", targetOut: "libdemo:amd64\tinstall ok installed\nlibdemo:arm64\tinstall ok installed\n", sentinelOut: sentinel, want: DpkgUnknown},
		{name: "single unqualified architecture", pkg: "libdemo", targetOut: "libdemo:amd64\tinstall ok installed\n", sentinelOut: sentinel, want: DpkgPresent},
		{name: "qualified architecture exact", pkg: "libdemo:arm64", targetOut: "libdemo:arm64\tinstall ok installed\n", sentinelOut: sentinel, want: DpkgPresent},
		{name: "dpkg target deduplicated", pkg: "dpkg", targetOut: sentinel, sentinelOut: sentinel, want: DpkgPresent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := run.Result{Stdout: []byte(tc.targetOut), ExitCode: tc.targetExit}
			sentinelResult := run.Result{Stdout: []byte(tc.sentinelOut), ExitCode: tc.sentinelExit}
			if tc.pkg == "dpkg" {
				sentinelResult = target
			}
			got, _ := ObserveDpkgResults(tc.pkg, target, sentinelResult)
			if got != tc.want {
				t.Fatalf("ObserveDpkgResults() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestObserveDpkgResultsRejectsUncleanExecution(t *testing.T) {
	boom := errors.New("probe failed")
	goodTarget := run.Result{Stdout: []byte("demo\tinstall ok installed\n")}
	goodSentinel := run.Result{Stdout: []byte("dpkg\tinstall ok installed\n")}
	for _, pair := range [][2]run.Result{
		{{Stdout: []byte("demo\tinstall ok installed\n"), ExitCode: 2}, goodSentinel},
		{{Err: boom}, goodSentinel},
		{{WaitErr: boom}, goodSentinel},
		{goodTarget, {ExitCode: 2}},
		{goodTarget, {Err: boom}},
		{goodTarget, {WaitErr: boom}},
		{{Stdout: []byte("demo\tinstall ok installed")}, goodSentinel},
	} {
		if got, _ := ObserveDpkgResults("demo", pair[0], pair[1]); got != DpkgUnknown {
			t.Errorf("ObserveDpkgResults(%#v) = %v, want unknown", pair, got)
		}
	}
}
