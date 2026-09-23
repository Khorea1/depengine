# Native platform smoke tests

`native-lifecycle.sh` is a POSIX `sh` harness that exercises the same minimal
lifecycle contract against a real package manager on an ephemeral native
runner:

1. validate the fixture;
2. assert the package is initially absent;
3. install it through depengine;
4. verify it with a live adapter check and read status;
5. install it again to exercise idempotency;
6. remove it and verify it is absent again.

Each platform fixture should use a small package that is not part of the runner
image. The harness refuses to continue when the package is already installed so
it never removes software it did not install itself.

The harness isolates depengine config and state through temporary
`XDG_CONFIG_HOME` and `XDG_STATE_HOME` directories. Package-manager mutations
still happen on the runner or VM itself, so this suite is intended for
disposable environments.

Current native lifecycle coverage uses `native-hello.toml` on the
GitHub-hosted macOS runner (Homebrew) and the real FreeBSD VM (`pkg`).
OpenBSD uses `openbsd-pkgadd.toml` against `pkg_add`, NetBSD uses
`netbsd-pkgin.toml` against `pkgin`, and Windows uses
`windows-choco.toml` against Chocolatey. The BSD jobs also run the Go test
suite inside their VMs, so support is validated beyond cross-compilation.

Android has a separate real-emulator runtime smoke test. It cross-builds the CLI
with the Android NDK, executes it through ADB, and asserts that depengine's own
OS detection resolves the device as Android. This does not claim Termux native
package-manager lifecycle coverage; that remains a distinct integration layer.
