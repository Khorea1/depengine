# Testing, property checks, fuzzing and failure reproduction

Use the repository's established Go commands and CI configuration as the
source of truth. The scripts below wrap them; they do not replace the regular
unit, integration or platform lifecycle suites.

## Local fast checks

```sh
go build ./...
go vet ./...
go test ./...
scripts/check-quality.sh race
# With the pinned tools installed:
scripts/check-quality.sh staticcheck
scripts/check-quality.sh govulncheck
# Or all three (race first):
scripts/check-quality.sh all
```

The CI workflow pins `staticcheck` at `v0.8.2` and `govulncheck` at `v1.8.0`.
For a matching local environment, install them with:

```sh
go install honnef.co/go/tools/cmd/staticcheck@v0.8.2
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
```

`golangci-lint run` remains an additional canonical check; its existing
configuration already enables the `staticcheck` analyzer. A standalone run is
included to retain an independently versioned diagnostic surface. The Go
race detector requires a working C toolchain and `CGO_ENABLED=1`; it does not
prove the absence of every concurrency bug. `govulncheck` needs access to its
vulnerability database, so a failed network lookup is **not** a clean scan.

## Properties and sequence testing

`internal/plan/reconciliation_rapid_test.go` uses
[`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid) to generate and
shrink sequences of observations, interruption/recovery phases and ownership
releases across schema-like dependent identities, adapter variants, package
identities and versions. The checks enforce:

- Inconclusive `unknown`/`broken` observations cannot select an install,
  upgrade or retry removal; one successful observation never grants authority
  to a later failed probe.
- Interrupted immutable-version replacement retries removing the prior version
  only when WAL phase and *two independent observations* allow it. A changed,
  conflicting or partially known installed identity must block removal.
- Release of a resource claimed by multiple dependents does not approve cleanup
  while a dependent remains. External ownership never approves cleanup.
  Unverified prospective target overlap is not treated as disjoint.
- `remove` and `undo` block an observed `drifted` identity (including a
  different package/version), and preserve tracking rather than deleting
  another physical target; explicit, proven absence is a distinct state.

Run or replay these tests with normal Go commands:

```sh
go test ./internal/plan -run '^TestRapid' -count=1 -v
go test ./internal/plan -run '^TestRapidReconciliationSequences$' -rapid.seed=12345 -rapid.checks=1000 -count=1
```

On a failure, copy the exact `-rapid.seed` replay command and the minimal
counterexample in the test log. Specify `-rapid.checks=N` when expanding a
local search. Keep a deterministic regression test for each confirmed bug,
rather than depending on the random seed remaining in CI logs forever.

### Ownership boundary

These tests validate the *currently implemented* per-resource ownership
projections and independent verification. **They do not establish global
cross-schema physical ownership safety.** In particular, separate schema
lockfiles, logical tool names, and persisted dependent counts are not proof
that two adapters address distinct physical resources. ADR-010 still requires
complete observed identity/effect evidence, host transaction enforcement and
revalidation at every destructive boundary (including fallback/batch/recovery)
before cross-schema destructive operations can be considered safe. Do not
convert unknown overlap into permission to delete.

## Native Go fuzz targets

`scripts/fuzz-targets.txt` is the authoritative inventory. The existing
`internal/tools/fuzzall` harness verifies both the manifest and discoverable
targets before running each one. New coverage includes personal TOML manifests,
`pipx` JSON and `snap` tabular output, and subprocess capture/completeness
failures. Previously registered fuzz targets continue covering schemas,
reconciliation, resource refcounts, archive paths, journaling and state.

Run a short local pass across all targets:

```sh
go test ./internal/tools/fuzzall
go run ./internal/tools/fuzzall -fuzztime=3s
```

Focus a longer campaign on the implicated package/target:

```sh
go test ./internal/ecosystem -run '^$' -fuzz '^FuzzPipxInventoryObservation$' -fuzztime=5m
go test ./internal/config -run '^$' -fuzz '^FuzzParsePersonalManifest$' -fuzztime=5m
```

In CI, the regular workflow uses a bounded 3-second smoke campaign **per
registered target**, after unit/race tests. The separate `Extended fuzzing`
workflow runs each target for two minutes on a weekly schedule or manually;
it does not block ordinary pull requests. It uploads persisted fuzz failures
as job artifacts when available. Avoid launching long fuzz campaigns as part
of each push or pre-commit hook.

### Reproduce and minimize

When the fuzz engine reports a new failure, it writes a file under
`<package>/testdata/fuzz/<FuzzTarget>/<hash>` and prints the replay command.
Run it in the same Go version/environment with:

```sh
go test ./internal/ecosystem -run '^FuzzPipxInventoryObservation/<hash>$' -count=1 -v
```

Replace `<hash>` with the exact file name reported by Go. To replay the entire
seed and regression corpus for a target, omit the `/hash` suffix and run the
named fuzz target as a normal test. Failing corpus entries should be reviewed,
minimized and committed only when they reproduce the bug without credentials,
private paths or sensitive subprocess data. A fixed deterministic unit test
is also appropriate when it clarifies the violated invariant. Retain at least
one seed per meaningful input class, but avoid huge/machine-specific corpora.

When expanding fuzz coverage, register every new `Fuzz*` function in the
manifest. Treat parser errors as legitimate outcomes; test the stronger
security property that malformed, truncated or failed observations never
silently become verified absence/presence. Do not make fuzzing depend on real
package managers, mutable network state or timing-sensitive processes.

## Mutation testing

Not part of the default toolchain. Go mutation frameworks exist, but there is
no demonstrated baseline here showing useful surviving mutants beyond the
property and fuzz checks, and compatibility/cost for the current Go toolchain
has not been established. Reconsider with a small, reproducible pilot in
`internal/plan` only if it finds meaningful blind spots and can run reliably
without inflating the fast CI gate.
