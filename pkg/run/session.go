package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// This file implements a sudo *session*: one interactive password prompt
// per execution, kept alive in the background, instead of one prompt per
// elevated command.
//
// Why this is needed: OSExecRunner.Run (runner.go) always captures
// stdout/stderr into buffers and never attaches Stdin. That is correct for
// every non-interactive command the engine runs, but it means a `sudo`
// invoked through Run can never actually receive a typed password — its
// Stdin is /dev/null, so it fails auth (or refuses outright with "no tty
// present") instead of prompting. EnsureSudo below is the one deliberate
// exception: it runs `sudo -v` with the real os.Stdin/Stdout/Stderr
// attached, so the single password prompt of the run reaches the user's
// actual terminal. Every elevated command after that goes through the
// normal buffered Runner using `sudo -n` (see native/command.go), which
// never blocks on input — it just relies on the cached credential that
// EnsureSudo obtained and KeepAlive renews.

// keepAliveInterval must be comfortably under sudo's default
// timestamp_timeout (5 minutes) so a slow build between two installs never
// lets the cached credential expire.
const keepAliveInterval = 45 * time.Second

// sudoNoPasswdOK reports whether sudo can run without ever prompting
// (NOPASSWD configured, or credential already cached from a prior session).
// This is the one legitimate use of `sudo -n` as a probe: it fails closed
// instead of prompting, so it is safe to call from a non-interactive
// context.
func sudoNoPasswdOK() bool {
	return exec.Command("sudo", "-n", "true").Run() == nil
}

// isInteractive reports whether the process's stdin is a real terminal.
// A wrapper spawned via exec.Command().Output(), a cron job, or an SSH
// session opened without `-t` all fail this check — and in every one of
// those cases, asking sudo to prompt would just hang or fail.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// EnsureSudo obtains a sudo credential for the process, prompting the user
// at most once. It is a no-op when already root or when sudo is already
// usable without a password. When a password is required but no TTY is
// available, it fails fast with an actionable message instead of hanging
// on a prompt nobody can answer.
func EnsureSudo(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if sudoNoPasswdOK() {
		return nil
	}
	if !isInteractive() {
		user := os.Getenv("USER")
		if user == "" {
			user = "$USER"
		}
		return fmt.Errorf(
			"root access required but no TTY is attached to request a password; "+
				"reconnect with a TTY (e.g. ssh -t) or add NOPASSWD rules for %s in /etc/sudoers.d/", user)
	}

	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// KeepAlive renews the cached sudo credential every keepAliveInterval until
// ctx is cancelled. It always uses `sudo -n`, so it can never itself
// trigger a prompt — if the credential already expired (e.g. the caller
// never called EnsureSudo, or the user's clock/sudoers config is unusual),
// the renewal simply fails silently and the next elevated command will
// surface the real error.
//
// Intended usage: call EnsureSudo once, then run KeepAlive in a goroutine
// for the lifetime of the operation that needs elevation.
func KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = exec.Command("sudo", "-n", "-v").Run()
		}
	}
}
