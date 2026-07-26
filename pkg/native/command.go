package native

import (
	"strings"

	"github.com/Khorea1/depengine/pkg/run"
)

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

// BuildBatchInstallCmd builds a single install command for multiple packages.
// It finds the {pkg} placeholder in the InstallCmd template and replaces it
// with all package names as separate argv entries. Returns nil if the clan
// has no known native manager — the caller then falls through to per-tool install.
//
// Package names are validated against a safe pattern before inclusion.
// The caller MUST ensure all packages share the same clan.
func BuildBatchInstallCmd(clan string, pkgs []string) []string {
	nm, ok := Lookup(clan)
	if !ok {
		return nil
	}
	if !nm.AtomicBatch {
		return nil // skip batching for non-atomic managers
	}

	// Find the {pkg} placeholder position and replace it with all packages.
	cmd := make([]string, 0, len(nm.InstallCmd)-1+len(pkgs))
	for _, arg := range nm.InstallCmd {
		if strings.Contains(arg, "{pkg}") {
			// Replace {pkg} with the first package, then add the rest.
			first := strings.ReplaceAll(arg, "{pkg}", pkgs[0])
			cmd = append(cmd, first)
			for i := 1; i < len(pkgs); i++ {
				cmd = append(cmd, pkgs[i])
			}
		} else {
			cmd = append(cmd, arg)
		}
	}

	return withSudo(cmd, nm.SudoRequired)
}

// IsBatchCapable reports whether the clan's native manager supports
// multi-package batch install (AtomicBatch == true).
func IsBatchCapable(clan string) bool {
	nm, ok := Lookup(clan)
	return ok && nm.AtomicBatch
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
	if prefix := run.ElevationPrefix(); prefix != nil {
		return append(prefix, cmd...)
	}
	// No working elevation — return as-is and let the call fail with
	// a clear permission error rather than silently succeeding with
	// insufficient privileges.
	return cmd
}

