package ecosystem

import (
	"context"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// snap list without a package filter is a complete inventory. A filtered
// invocation's non-zero status cannot distinguish an absent snap from a daemon
// failure, which is why the V2 observation uses the full list instead.
func (a *BaseAdapter) observeSnapInventory(ctx context.Context, rn run.Runner, pkg string) plan.Observation {
	res := a.runConfiguredBinary(ctx, rn, "list", "--color=never", "--unicode=never")
	if err := run.CheckResultCompleteOutput(res, "snap: installed package inventory"); err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: err.Error()}
	}
	if len(res.Stdout) == 0 && strings.TrimSpace(string(res.Stderr)) == "No snaps are installed yet. Try 'snap install hello-world'." {
		return plan.Observation{Presence: plan.PresenceAbsent, Identity: plan.ObservedIdentity{Package: pkg}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
	}
	const header = "Name Version Rev Tracking Publisher Notes"
	lines := strings.Lines(string(res.Stdout))
	row := 0
	seen := make(map[string]bool)
	var installed plan.ObservedIdentity
	for line := range lines {
		fields := strings.Fields(line)
		if row == 0 {
			if strings.Join(fields, " ") != header {
				return plan.Observation{Presence: plan.PresenceBroken, Detail: "snap: invalid installed package inventory header"}
			}
			row++
			continue
		}
		if len(fields) != 6 || seen[fields[0]] || !validSnapTracking(fields[3]) {
			return plan.Observation{Presence: plan.PresenceBroken, Detail: "snap: malformed or duplicate package inventory row"}
		}
		seen[fields[0]] = true
		if fields[0] == pkg {
			installed = plan.ObservedIdentity{Package: pkg, Version: fields[1], Revision: fields[2], Channel: fields[3]}
		}
		row++
	}
	if row == 0 {
		return plan.Observation{Presence: plan.PresenceBroken, Detail: "snap: missing installed package inventory header"}
	}
	if installed.Package == "" {
		return plan.Observation{Presence: plan.PresenceAbsent, Identity: plan.ObservedIdentity{Package: pkg}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
	}
	if strings.Contains(installed.Channel, "…") {
		res := a.runConfiguredBinary(ctx, rn, "info", pkg)
		if err := run.CheckResultCompleteOutput(res, "snap: tracking channel"); err != nil {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: err.Error()}
		}
		channel, ok := snapTrackingFromInfo(string(res.Stdout), pkg)
		listedPrefix, listedOK := snapChannelPrefix(installed.Channel)
		infoPrefix, infoOK := snapChannelPrefix(channel)
		if !ok || channel == "-" || !listedOK || !infoOK || infoPrefix != listedPrefix {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: "snap: info tracking channel is inconsistent with list"}
		}
		recheck := a.runConfiguredBinary(ctx, rn, "list", "--color=never", "--unicode=never", pkg)
		if err := run.CheckResultCompleteOutput(recheck, "snap: installed package recheck"); err != nil {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: err.Error()}
		}
		if !snapListIdentityMatches(string(recheck.Stdout), installed) {
			return plan.Observation{Presence: plan.PresenceUnknown, Detail: "snap: installed identity changed during tracking query"}
		}
		installed.Channel = channel
	}
	known := []plan.IdentityField{plan.FieldPackage, plan.FieldVersion, plan.FieldRevision}
	if installed.Channel != "-" {
		known = append(known, plan.FieldChannel)
	}
	return plan.Observation{Presence: plan.PresencePresent, Identity: installed, KnownFields: known}
}

func snapListIdentityMatches(output string, want plan.ObservedIdentity) bool {
	lines := strings.Lines(output)
	row := 0
	found := false
	for line := range lines {
		fields := strings.Fields(line)
		if row == 0 {
			if strings.Join(fields, " ") != "Name Version Rev Tracking Publisher Notes" {
				return false
			}
			row++
			continue
		}
		if len(fields) != 6 {
			return false
		}
		if fields[0] == want.Package {
			if found || fields[1] != want.Version || fields[2] != want.Revision || fields[3] != want.Channel {
				return false
			}
			found = true
		}
		row++
	}
	return row > 0 && found
}

// A tracking channel has a track, one of Snap's four risks, and an
// optional branch. Reject a malformed channel instead of reporting drift
// based on a diagnostic masquerading as inventory data.
func validSnapTracking(channel string) bool {
	if channel == "-" {
		return true
	}
	parts := strings.Split(channel, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, " \t\r\n") {
			return false
		}
	}
	switch parts[1] {
	case "stable", "candidate", "beta", "edge":
		return true
	}
	return false
}

// A successful --list-extensions with an empty body means no installed
// extensions. All other output must be a list of publisher.name IDs, rather
// than warnings or a truncated list that could invent presence or absence.
func (a *BaseAdapter) observeEditorExtensions(ctx context.Context, rn run.Runner, pkg string) plan.Observation {
	res := a.runConfiguredBinary(ctx, rn, "--list-extensions")
	if err := run.CheckResultCompleteOutput(res, a.config.KindName+": extension inventory"); err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: err.Error()}
	}
	seen := make(map[string]bool)
	found := false
	for line := range strings.Lines(string(res.Stdout)) {
		id := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if !validEditorExtensionID(id) || seen[key] {
			return plan.Observation{Presence: plan.PresenceBroken, Detail: a.config.KindName + ": malformed or duplicate extension inventory"}
		}
		seen[key] = true
		if strings.EqualFold(id, pkg) {
			found = true
		}
	}
	if found {
		return plan.Observation{Presence: plan.PresencePresent, Identity: plan.ObservedIdentity{Package: pkg}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
	}
	return plan.Observation{Presence: plan.PresenceAbsent, Identity: plan.ObservedIdentity{Package: pkg}, KnownFields: []plan.IdentityField{plan.FieldPackage}}
}

func validEditorExtensionID(id string) bool {
	parts := strings.Split(id, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, ch := range part {
			if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
				return false
			}
		}
	}
	return true
}
