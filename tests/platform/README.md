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

`native-failure.sh` complements the success lifecycle with a package name
reserved for the test suite. It attempts installation through the real native selection path, then proves
that live observation remains absent and no removable tracked state was
committed. Candidate exhaustion and manager errors are both valid outcomes.

Current native lifecycle coverage uses `native-hello.toml` on the
GitHub-hosted macOS runner (Homebrew) and the real FreeBSD VM (`pkg`).
OpenBSD uses `openbsd-pkgadd.toml` against `pkg_add`, NetBSD uses
`netbsd-pkgin.toml` against `pkgin`, and Windows uses
`windows-choco.toml` against Chocolatey. The BSD jobs also run the Go test
suite inside their VMs, so support is validated beyond cross-compilation.

Android has two real-emulator layers in the same job. The first cross-builds
the CLI with the Android NDK, executes it through ADB, and asserts that
depengine's own OS detection resolves the device as Android. The second installs
a pinned official Termux GitHub-debug APK and reuses the native lifecycle
harnesses inside the app sandbox to exercise the real Termux `pkg`
install/check/idempotency/remove contract plus failed-install state handling.
The Android APK adapter's interactive package-installer handoff remains outside
this automated lifecycle coverage.


`native-rpm-smoke.sh` is a disposable-container conformance probe for RPM-backed
observation. It probes installed and absent packages, an unavailable database
whose target and sentinel both report absence, a malformed query that emits a
diagnostic while the sentinel succeeds, and a damaged database through
depengine status/remove. The final check requires unknown/broken state and
retained tracking. Build the CLI from the repository root, then run one fresh
container per image (the script damages its database):

```sh
go build -o /tmp/depengine-rpm .
docker run --rm -e DEPENGINE_BIN=/depengine -v /tmp/depengine-rpm:/depengine:ro -v "$PWD/tests/platform/native-rpm-smoke.sh:/probe.sh:ro" --entrypoint /bin/sh fedora:latest /probe.sh dnf bash
docker run --rm -e DEPENGINE_BIN=/depengine -v /tmp/depengine-rpm:/depengine:ro -v "$PWD/tests/platform/native-rpm-smoke.sh:/probe.sh:ro" --entrypoint /bin/sh opensuse/leap:15.6 /probe.sh zypper bash
```

The probe records RPM/backend versions and verifies a target and sentinel query
through the same production adapter used by default native and explicit dnf /
zypper methods. Container results currently establish Fedora RPM 6.0.2 with
SQLite and openSUSE Leap 15.6 RPM 4.14.3 with NDB; they do not imply conformance
for unprobed RPM releases or other distributions.
