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

// TODO(próxima etapa): adapters de cargo, go, pip, pipx, uv, git e http —
// esses não têm "família de distro", tratam-se por linguagem/ferramenta
// e não entram neste pacote. Cada um vira pkg/<nome> com a mesma forma:
// Lookup(<clan-equivalente>) + BuildXxxCmd. A executor decide a ordem.
