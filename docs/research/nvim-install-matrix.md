# Neovim installation capability matrix

The original matrix identified the requirements below. See the parseable
companion file, [`nvim-install-matrix.toml`](nvim-install-matrix.toml).

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

These cases fail closed: `http` rejects platform installer extensions, archive
entrypoints must exist before commit, and asset matching rejects zero or
multiple results. A failed source or lazy dependency discards its candidate and
allows fallback to continue.

One correction from the original research: Scoop tracks the `main` bucket and
uses it by default. `scoop bucket add main` is not a general prerequisite.
`scoop-bucket` remains useful for optional and custom buckets. See the
[official Scoop bucket documentation](https://github.com/ScoopInstaller/Scoop/wiki/Buckets).

The schema does not expose arbitrary manager arguments; the engine owns
non-interactive flags. depengine does not edit shell profiles or Windows
`PATH`. When a launcher directory is absent from `PATH`, installation prints
the directory to add.
