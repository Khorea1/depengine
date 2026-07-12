package native

import "strings"

// BuildInstallCmd substitutes "{pkg}" and prepends sudo when the manager
// requires it. Returns nil if the clan has no known native manager — the
// caller then advances method_order.
func BuildInstallCmd(clan, pkg string) []string {
	nm, ok := Lookup(clan)
	if !ok {
		return nil
	}
	return withSudo(substitutePkg(nm.InstallCmd, pkg), nm.SudoRequired)
}

// BuildCheckCmd substitutes "{pkg}" in the check command. Never adds sudo
// — checking whether something is installed should never need privilege.
func BuildCheckCmd(clan, pkg string) []string {
	nm, ok := Lookup(clan)
	if !ok {
		return nil
	}
	return substitutePkg(nm.CheckCmd, pkg)
}

// BuildRemoveCmd substitutes "{pkg}" in the remove command. Returns nil if
// the clan has no known native manager or no remove command.
func BuildRemoveCmd(clan, pkg string) []string {
	nm, ok := Lookup(clan)
	if !ok {
		return nil
	}
	return withSudo(substitutePkg(nm.RemoveCmd, pkg), nm.SudoRequired)
}

// BuildSyncCmd returns the index-sync command, if the manager requires one.
// The engine runs this at most once per execution, never per package.
func BuildSyncCmd(clan string) []string {
	nm, ok := Lookup(clan)
	if !ok || !nm.NeedsSync {
		return nil
	}
	return withSudo(nm.SyncCmd, nm.SudoRequired)
}

func substitutePkg(cmd []string, pkg string) []string {
	out := make([]string, len(cmd))
	for i, arg := range cmd {
		out[i] = strings.ReplaceAll(arg, "{pkg}", pkg)
	}
	return out
}

func withSudo(cmd []string, sudoRequired bool) []string {
	if !sudoRequired {
		return cmd
	}
	return append([]string{"sudo"}, cmd...)
}

