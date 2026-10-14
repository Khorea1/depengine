package native

import (
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

type DpkgPresence uint8

const (
	DpkgUnknown DpkgPresence = iota
	DpkgAbsent
	DpkgPresent
)

const dpkgStatusFormat = "-f=${binary:Package}\t${Status}\n"

// BuildDpkgObserveCmd queries only the requested package using dpkg's
// structured status fields. Package syntax is restricted to literal names so
// dpkg-query cannot interpret a glob or option.
func BuildDpkgObserveCmd(pkg string) []string {
	if !validDpkgPackage(pkg) {
		return nil
	}
	return []string{"dpkg-query", "-W", dpkgStatusFormat, pkg}
}

// BuildDpkgSentinelCmd reads the dpkg package record that validates the
// supported Debian/Termux status database through dpkg-query's default path.
func BuildDpkgSentinelCmd() []string {
	return []string{"dpkg-query", "-W", dpkgStatusFormat, "dpkg"}
}

// ObserveDpkgResults requires both a clean target query and a healthy sentinel.
// A missing target is absent only when the separate sentinel query succeeds.
func ObserveDpkgResults(pkg string, targetResult, sentinelResult run.Result) (DpkgPresence, string) {
	if !validDpkgPackage(pkg) {
		return DpkgUnknown, "invalid dpkg package identity"
	}
	if targetResult.Err != nil || targetResult.WaitErr != nil || sentinelResult.Err != nil || sentinelResult.WaitErr != nil {
		return DpkgUnknown, "dpkg-query execution did not complete cleanly"
	}
	if targetResult.ExitCode != 0 && targetResult.ExitCode != 1 {
		return DpkgUnknown, "dpkg-query reported an operational failure"
	}
	if sentinelResult.ExitCode != 0 {
		return DpkgUnknown, "dpkg status-database sentinel query failed"
	}

	sentinelRows := parseDpkgRows(string(sentinelResult.Stdout))
	if !sentinelRows.valid || sentinelRows.count != 1 || !isDpkgPackage(sentinelRows.first.name) || dpkgStatus(sentinelRows.first.status) != dpkgStatusInstalled {
		return DpkgUnknown, "dpkg status-database sentinel is missing, ambiguous, or unhealthy"
	}

	if targetResult.ExitCode == 1 {
		if len(targetResult.Stdout) != 0 {
			return DpkgUnknown, "dpkg-query failed after returning target data"
		}
		return DpkgAbsent, "package is absent from the healthy dpkg status database"
	}
	targetRows := parseDpkgRows(string(targetResult.Stdout))
	if !targetRows.valid || targetRows.count == 0 {
		return DpkgUnknown, "dpkg-query returned malformed or empty target status data"
	}
	if targetRows.count > 1 {
		return DpkgUnknown, "dpkg package identity is ambiguous across architectures"
	}
	if !dpkgNameMatches(pkg, targetRows.first.name) {
		return DpkgUnknown, "dpkg-query returned a different package identity"
	}
	switch dpkgStatus(targetRows.first.status) {
	case dpkgStatusInstalled:
		return DpkgPresent, "package is installed"
	case dpkgStatusAbsent:
		return DpkgAbsent, "package has no installed files"
	default:
		return DpkgUnknown, "package status is incomplete or unsupported"
	}
}

type dpkgRow struct {
	name   string
	status string
}

type dpkgRows struct {
	first dpkgRow
	count int
	valid bool
}

func parseDpkgRows(output string) dpkgRows {
	if output == "" || !strings.HasSuffix(output, "\n") {
		return dpkgRows{}
	}
	output = strings.TrimSuffix(output, "\n")
	if strings.HasSuffix(output, "\n") {
		return dpkgRows{}
	}
	rows := dpkgRows{valid: true}
	for output != "" {
		line, rest, found := strings.Cut(output, "\n")
		if found {
			output = rest
		} else {
			output = ""
		}
		name, status, found := strings.Cut(line, "\t")
		if !found || name == "" || strings.Contains(status, "\t") || !validDpkgPackage(name) || !validDpkgStatus(status) {
			return dpkgRows{}
		}
		if rows.count == 0 {
			rows.first = dpkgRow{name: name, status: status}
		}
		rows.count++
	}
	return rows
}

func validDpkgPackage(value string) bool {
	name, arch, qualified := strings.Cut(value, ":")
	if !validDpkgName(name) {
		return false
	}
	return !qualified || validDpkgArch(arch)
}

func validDpkgName(value string) bool {
	if len(value) < 2 || !dpkgAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !dpkgAlphaNumeric(c) && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func validDpkgArch(value string) bool {
	if value == "" || !dpkgAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if !dpkgAlphaNumeric(c) && c != '-' {
			return false
		}
	}
	return true
}

func dpkgAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func validDpkgStatus(status string) bool {
	desired, rest, ok := strings.Cut(status, " ")
	if !ok || !validDpkgDesired(desired) {
		return false
	}
	errorFlag, actual, ok := strings.Cut(rest, " ")
	return ok && validDpkgError(errorFlag) && validDpkgActual(actual)
}

func validDpkgDesired(value string) bool {
	switch value {
	case "unknown", "install", "hold", "deinstall", "purge":
		return true
	default:
		return false
	}
}

func validDpkgError(value string) bool {
	return value == "ok" || value == "reinstreq"
}

func validDpkgActual(value string) bool {
	switch value {
	case "not-installed", "config-files", "half-installed", "unpacked", "half-configured", "triggers-awaited", "triggers-pending", "installed":
		return true
	default:
		return false
	}
}

type dpkgStatusKind uint8

const (
	dpkgStatusUnknown dpkgStatusKind = iota
	dpkgStatusInstalled
	dpkgStatusAbsent
)

func dpkgStatus(status string) dpkgStatusKind {
	_, rest, ok := strings.Cut(status, " ")
	if !ok {
		return dpkgStatusUnknown
	}
	errorFlag, actual, ok := strings.Cut(rest, " ")
	if !ok || errorFlag != "ok" {
		return dpkgStatusUnknown
	}
	switch actual {
	case "installed":
		return dpkgStatusInstalled
	case "not-installed", "config-files":
		return dpkgStatusAbsent
	default:
		return dpkgStatusUnknown
	}
}

func dpkgNameMatches(requested, actual string) bool {
	if requested == actual {
		return true
	}
	return !strings.Contains(requested, ":") && strings.HasPrefix(actual, requested+":")
}

func isDpkgPackage(name string) bool {
	return name == "dpkg"
}
