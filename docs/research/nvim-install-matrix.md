# Neovim installation matrix: resolved capability audit

The original matrix exposed seven structural gaps. They are now represented by
reusable schema primitives; the parseable companion file is
[`nvim-install-matrix.toml`](nvim-install-matrix.toml).

| Requirement | Current contract |
|---|---|
| Nested `.tar.gz` / `.zip` payload | `strip_components`, owned `extract_to`, `entrypoints`, `link_dir` |
| Windows MSI | dedicated `msi` method with exact `product_name` / optional `publisher` identity |
| Snap confinement/channel | typed `confinement` and `channel` fields |
| Chocolatey prerelease | typed `prerelease` field |
| PPA/COPR/Scoop bucket/Brew tap | candidate-scoped, idempotent `sources` |
| Candidate-only prerequisite | method-level `requires` plus `dependency_only` tools |
| Custom-build removal | validated `managed_paths` ownership |
| GitHub asset + specialized post-processing | shared `url` or `repo + asset` reference for GitHub, AppImage, Android and MSI |

The dangerous silent cases now fail closed: `http` rejects platform installer
extensions, archive entrypoints must exist before commit, asset matching rejects
zero or multiple results, and a failed source or lazy dependency discards only
the candidate so fallback remains possible.

One correction from the original research: Scoop tracks the `main` bucket and
uses it by default. `scoop bucket add main` is not a general prerequisite.
`scoop-bucket` remains useful for optional and custom buckets. See the
[official Scoop bucket documentation](https://github.com/ScoopInstaller/Scoop/wiki/Buckets).

The design deliberately does not expose arbitrary manager arguments. The
engine owns non-interactive flags. It also never edits shell profiles or
Windows PATH silently: when a launcher directory is absent from `PATH`,
installation prints the exact directory the operator must add.
