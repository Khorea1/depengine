package native

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestBuildRpmObserveCmd(t *testing.T) {
	want := []string{"rpm", "-q", "--qf=%{NAME}\t%{ARCH}\n", "--", "glibc.x86_64"}
	if got := BuildRpmObserveCmd("glibc.x86_64"); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildRpmObserveCmd() = %#v, want %#v", got, want)
	}
	for _, invalid := range []string{"", "lib x", "rpm:2.x86_64", "name/version", "--help", "-name", ".hidden"} {
		if got := BuildRpmObserveCmd(invalid); got != nil {
			t.Errorf("BuildRpmObserveCmd(%q) = %#v, want nil", invalid, got)
		}
	}
}

func TestBuildRpmSentinelCmd(t *testing.T) {
	want := []string{"rpm", "-q", "--qf=%{NAME}\t%{ARCH}\n", "--", "filesystem"}
	if got := BuildRpmSentinelCmd(); !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildRpmSentinelCmd() = %#v, want %#v", got, want)
	}
}

func TestObserveRpmResults(t *testing.T) {
	const sentinel = "filesystem\tx86_64\n"
	cases := []struct {
		name, pkg, targetOut, targetErr, sentinelOut, sentinelErr string
		targetExit, sentinelExit                                  int
		want                                                      RpmPresence
	}{
		{name: "installed", pkg: "bash", targetOut: "bash\tx86_64\n", sentinelOut: sentinel, want: RpmPresent},
		{name: "explicit architecture selector", pkg: "glibc.x86_64", targetOut: "glibc\tx86_64\n", sentinelOut: sentinel, want: RpmPresent},
		{name: "absent with C locale diagnostic", pkg: "missing", targetExit: 1, targetOut: "package missing is not installed\n", sentinelOut: sentinel, want: RpmAbsent},
		{name: "empty exit one is ambiguous", pkg: "missing", targetExit: 1, sentinelOut: sentinel, want: RpmUnknown},
		{name: "target query error shares exit one", pkg: "missing", targetExit: 1, targetErr: "rpm query failed", sentinelOut: sentinel, want: RpmUnknown},
		{name: "noncanonical localized diagnostic", pkg: "missing", targetExit: 1, targetOut: "package missing est introuvable\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "query error with zero exit and stderr", pkg: "bash", targetErr: "error: incorrect format", sentinelOut: sentinel, want: RpmUnknown},
		{name: "database query failure", pkg: "missing", targetExit: 1, targetOut: "package missing is not installed\n", targetErr: "rpmdb damaged", sentinelExit: 1, sentinelErr: "rpmdb damaged", want: RpmUnknown},
		{name: "malformed target row", pkg: "bash", targetOut: "bash\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "empty successful target query", pkg: "missing", sentinelOut: sentinel, want: RpmUnknown},
		{name: "malformed target extra field", pkg: "bash", targetOut: "bash\tx86_64\textra\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "target without final newline", pkg: "bash", targetOut: "bash\tx86_64", sentinelOut: sentinel, want: RpmUnknown},
		{name: "target error despite healthy sentinel", pkg: "bash", targetExit: 2, sentinelOut: sentinel, want: RpmUnknown},
		{name: "target failure with row output", pkg: "bash", targetExit: 1, targetOut: "bash\tx86_64\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "different package identity", pkg: "bash", targetOut: "coreutils\tx86_64\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "multiple architectures ambiguous", pkg: "glibc", targetOut: "glibc\tx86_64\nglibc\ti686\n", sentinelOut: sentinel, want: RpmUnknown},
		{name: "missing filesystem sentinel", pkg: "bash", targetOut: "bash\tx86_64\n", want: RpmUnknown},
		{name: "wrong filesystem sentinel", pkg: "bash", targetOut: "bash\tx86_64\n", sentinelOut: "glibc\tx86_64\n", want: RpmUnknown},
		{name: "ambiguous filesystem sentinel", pkg: "bash", targetOut: "bash\tx86_64\n", sentinelOut: "filesystem\tx86_64\nfilesystem\ti686\n", want: RpmUnknown},
		{name: "sentinel diagnostics", pkg: "bash", targetOut: "bash\tx86_64\n", sentinelOut: sentinel, sentinelErr: "warning", want: RpmUnknown},
		{name: "sentinel target deduplicated", pkg: "filesystem", targetOut: sentinel, sentinelOut: sentinel, want: RpmPresent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := run.Result{Stdout: []byte(tc.targetOut), Stderr: []byte(tc.targetErr), ExitCode: tc.targetExit}
			sentinelResult := run.Result{Stdout: []byte(tc.sentinelOut), Stderr: []byte(tc.sentinelErr), ExitCode: tc.sentinelExit}
			if tc.pkg == "filesystem" {
				sentinelResult = target
			}
			got, _ := ObserveRpmResults(tc.pkg, target, sentinelResult)
			if got != tc.want {
				t.Fatalf("ObserveRpmResults() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestObserveRpmResultsRejectsUncleanExecution(t *testing.T) {
	boom := errors.New("probe failed")
	goodTarget := run.Result{Stdout: []byte("bash\tx86_64\n")}
	goodSentinel := run.Result{Stdout: []byte("filesystem\tx86_64\n")}
	for _, pair := range [][2]run.Result{
		{{Err: boom}, goodSentinel},
		{{WaitErr: boom}, goodSentinel},
		{goodTarget, {Err: boom}},
		{goodTarget, {WaitErr: boom}},
	} {
		if got, _ := ObserveRpmResults("bash", pair[0], pair[1]); got != RpmUnknown {
			t.Errorf("ObserveRpmResults(%#v) = %v, want unknown", pair, got)
		}
	}
}
