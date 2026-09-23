# Native platform smoke tests

`native-lifecycle.sh` exercises the same minimal lifecycle contract against a
real package manager on an ephemeral native runner:

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
still happen on the runner itself, so this suite is intended for disposable VMs
or similarly ephemeral environments.

Current coverage starts with Homebrew on the GitHub-hosted macOS runner. Future
BSD/Linux native runners can reuse this harness by adding a fixture; Windows can
either invoke it from Git Bash or use a thin PowerShell wrapper if native shell
behavior becomes part of the contract.
