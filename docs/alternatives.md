# Alternatives and positioning

depengine is one answer to "how does a project get its tools installed on
whatever machine clones it". Neighboring tools solve overlapping but distinct
problems. This page compares behavioral scope, not feature checklists, so you
can choose one tool or combine them.

## The depengine model

- A committed `schema.toml` declares tools per project; an optional personal
  manifest adds operator-local packages.
- For each tool, depengine selects among declared and inferred methods —
  native package managers, language package managers, GitHub release assets,
  direct downloads, Git builds, containers, and more — in priority order with
  per-tool fallbacks, and installs through the first method the current
  machine supports (`when` conditions and OS detection drive the choice).
- Lifecycle fields (`pre_install`, `post_install`, `build`) run per install
  transition; arbitrary-code execution stays behind the CLI's explicit
  authorization gate (see [security](security.md)).
- `depengine.lock` pins the mutable references the lock model supports
  (release tags, resolved checksums) for `--frozen-lockfile` installs; the
  exact coverage is defined by the
  [support boundary](support-boundary.md).
- Install state is recorded, so `status`, `check`, `upgrade`, `remove`, and
  `undo` operate on what depengine actually did.

depengine installs and records. It does not activate environments or switch
tool versions in the shell; it does not build its own package universe.

## Comparison

| Tool | Declares | Installs via | What it pins | Primary scope |
|---|---|---|---|---|
| depengine | tools per project (`schema.toml`) | whatever the host supports, with per-tool fallback across native managers, ecosystems, and artifacts | supported mutable references in `depengine.lock` | the same project tool set across distros and OSes |
| [mise](https://mise.jdx.dev) | tool versions per project (TOML) | its own backends — asdf plugins, registry-backed binaries, language package managers | exact tool versions, activated per project via env or shims | version management and activation of runtimes/CLIs |
| [aqua](https://aquaproj.dev) | CLI tools and versions (YAML registry) | downloads declared binaries from registries (GitHub releases, OCI, HTTP) with checksum and optional signature verification | exact binary versions plus checksums | declarative installation of binary tools from one registry |
| [Nix](https://nixos.org/manual/nix/stable/) | package expressions and environments | its own package set and content-addressed store, built from derivations | store paths — full closure reproducibility | a self-contained package universe and environment model |
| [Brewfile](https://docs.brew.sh/Bundle) | formulae, casks, and taps (manifest) | Homebrew only, via `brew bundle` | none — Homebrew installs current versions | declarative Homebrew installs on macOS and Linux |

## Where they compose

The overlap is smaller than the shared vocabulary suggests:

- **mise and asdf as a method, not a rival.** depengine's `asdf` method runs
  on either the `asdf` or the `mise` binary when present, so a project can
  keep using them for version-managed runtimes while depengine handles the
  rest of the tool set.
- **Homebrew is a native method.** `brew` installs through depengine's
  package-manager registry like any other native manager, with the same
  fallback and state model — not a separate bundle path.
- **Nix-style reproducibility is a different trade.** Nix guarantees closure
  reproducibility by owning the whole package graph; depengine guarantees
  declaration portability by driving each host's existing managers and pinning
  only what the lock model supports.

## Choosing

- Need version activation and switching for runtimes per project? mise (or
  asdf) — and depengine can drive it as a method.
- Need one pinned set of CLI binaries from a curated registry? aqua.
- Need whole-system or whole-environment reproducibility and don't mind
  adopting a package language? Nix.
- Need a Homebrew machine set up declaratively? Brewfile.
- Need one project declaration that installs through each machine's own
  package managers, with fallbacks, hooks, and recorded state? depengine.

Adapter coverage and guarantees are deliberately bounded; read
[support boundary](support-boundary.md) before relying on a specific method.
