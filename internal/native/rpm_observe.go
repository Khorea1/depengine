package native

import (
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

// RpmPresence is the authoritative installed-state answer for an
// RPM-backed package (Fedora dnf, SUSE zypper).
type RpmPresence uint8

const (
	RpmUnknown RpmPresence = iota
	RpmAbsent
	RpmPresent
)

const rpmQueryFormat = "--qf=%{NAME}\t%{ARCH}\n"

// BuildRpmObserveCmd queries one exact package identity with structured
// fields. Package syntax is restricted so rpm cannot interpret an option or
// reparse the argument.
func BuildRpmObserveCmd(pkg string) []string {
	if !validRpmName(pkg) {
		return nil
	}
	return []string{"rpm", "-q", rpmQueryFormat, "--", pkg}
}

// BuildRpmSentinelCmd queries the base filesystem record to establish that
// the RPM database is readable on supported Fedora and SUSE systems.
func BuildRpmSentinelCmd() []string {
	return []string{"rpm", "-q", rpmQueryFormat, "--", "filesystem"}
}

// ObserveRpmResults reports absence only for RPM's exact C-locale missing
// package diagnostic, with no stderr and a healthy structured sentinel. RPM
// uses exit status 1 for both absence and query/database failures.
func ObserveRpmResults(pkg string, targetResult, sentinelResult run.Result) (RpmPresence, string) {
	if !validRpmName(pkg) {
		return RpmUnknown, "invalid rpm package identity"
	}
	if targetResult.Err != nil || targetResult.WaitErr != nil || sentinelResult.Err != nil || sentinelResult.WaitErr != nil {
		return RpmUnknown, "rpm query execution did not complete cleanly"
	}
	if sentinelResult.ExitCode != 0 || len(sentinelResult.Stderr) != 0 {
		return RpmUnknown, "rpm database sentinel query failed"
	}
	sentinelRows := parseRpmRows(string(sentinelResult.Stdout))
	if !sentinelRows.valid || sentinelRows.count != 1 || sentinelRows.firstName != "filesystem" {
		return RpmUnknown, "rpm database sentinel is missing, ambiguous, or malformed"
	}

	switch targetResult.ExitCode {
	case 0:
		if len(targetResult.Stderr) != 0 {
			return RpmUnknown, "rpm package query returned diagnostics"
		}
		rows := parseRpmRows(string(targetResult.Stdout))
		if !rows.valid || rows.count == 0 {
			return RpmUnknown, "rpm returned malformed or empty package data"
		}
		if rows.count > 1 {
			return RpmUnknown, "rpm package identity is ambiguous across architectures"
		}
		if !rpmQueryMatchesPackage(pkg, rows.firstName, rows.firstArch) {
			return RpmUnknown, "rpm returned a different package identity"
		}
		return RpmPresent, "package is installed in the healthy RPM database"
	case 1:
		if isRpmAbsentOutput(pkg, targetResult.Stdout, targetResult.Stderr) {
			return RpmAbsent, "package is absent from the healthy RPM database"
		}
		return RpmUnknown, "rpm package query failed without a recognized absence result"
	default:
		return RpmUnknown, "rpm package query reported an operational failure"
	}
}

func isRpmAbsentOutput(pkg string, stdout, stderr []byte) bool {
	const prefix = "package "
	const suffix = " is not installed\n"
	if len(stderr) != 0 || len(stdout) != len(prefix)+len(pkg)+len(suffix) {
		return false
	}
	for i := range len(prefix) {
		if stdout[i] != prefix[i] {
			return false
		}
	}
	for i := range len(pkg) {
		if stdout[len(prefix)+i] != pkg[i] {
			return false
		}
	}
	for i := range len(suffix) {
		if stdout[len(prefix)+len(pkg)+i] != suffix[i] {
			return false
		}
	}
	return true
}

type rpmRows struct {
	firstName string
	firstArch string
	count     int
	valid     bool
}

func parseRpmRows(output string) rpmRows {
	if output == "" || !strings.HasSuffix(output, "\n") {
		return rpmRows{}
	}
	output = strings.TrimSuffix(output, "\n")
	if output == "" || strings.HasSuffix(output, "\n") {
		return rpmRows{}
	}
	rows := rpmRows{valid: true}
	for output != "" {
		line, rest, found := strings.Cut(output, "\n")
		if found {
			output = rest
		} else {
			output = ""
		}
		name, arch, found := strings.Cut(line, "\t")
		if !found || name == "" || strings.Contains(arch, "\t") || !validRpmName(name) || !validRpmArch(arch) {
			return rpmRows{}
		}
		if rows.count == 0 {
			rows.firstName, rows.firstArch = name, arch
		}
		rows.count++
	}
	return rows
}

func rpmQueryMatchesPackage(pkg, name, arch string) bool {
	if pkg == name {
		return true
	}
	dot := strings.LastIndexByte(pkg, '.')
	return dot > 0 && pkg[:dot] == name && pkg[dot+1:] == arch
}

// validRpmName restricts package names to RPM's literal package-name
// charset. RPM allows uppercase unlike dpkg; colons and whitespace are
// rejected so query identities cannot be reparsed as labels or options.
func validRpmName(value string) bool {
	if len(value) == 0 || len(value) > 256 || !rpmNameFirst(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !rpmNameChar(value[i]) {
			return false
		}
	}
	return true
}

func rpmNameFirst(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func rpmNameChar(c byte) bool {
	return rpmNameFirst(c) || c == '.' || c == '+' || c == '-' || c == '~'
}

func validRpmArch(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for i := range len(value) {
		if !rpmNameFirst(value[i]) {
			return false
		}
	}
	return true
}
