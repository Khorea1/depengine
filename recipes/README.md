# Recipe library

These standalone `recipe_version = 1` TOML files are the public, Git-versioned
catalog for depengine. They can be reviewed, copied, amended and contributed
without compiling a new depengine binary. Each `*.toml` file declares exactly
one `[packages.<name>]` entry and its filename matches that name.

| Recipe | Declared methods | Availability |
|---|---|---|
| [`golang.toml`](golang.toml) | Native managers, AUR and checksum-pinned Linux amd64 HTTP | Also bundled |
| [`nvim.toml`](nvim.toml) | Native managers, AUR, Flatpak, Snap, checksum-pinned AppImage/GitHub archives | Opt-in only |
| [`bat.toml`](bat.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`black.toml`](black.toml) | pipx, uv | Opt-in only |
| [`bottom.toml`](bottom.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`delta.toml`](delta.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`fd.toml`](fd.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`fzf.toml`](fzf.toml) | Native managers, AUR, Go | Opt-in only |
| [`gh.toml`](gh.toml) | Native managers, AUR, Go | Opt-in only |
| [`golangci-lint.toml`](golangci-lint.toml) | Native managers, AUR, Go | Opt-in only |
| [`helm.toml`](helm.toml) | Native managers, AUR, Go | Opt-in only |
| [`httpie.toml`](httpie.toml) | Native managers, pipx, uv | Opt-in only |
| [`hyperfine.toml`](hyperfine.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`jq.toml`](jq.toml) | Native managers, AUR | Opt-in only |
| [`just.toml`](just.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`k9s.toml`](k9s.toml) | Native managers, AUR, Go | Opt-in only |
| [`kubectl.toml`](kubectl.toml) | Native managers, checksum-pinned Linux/macOS raw binaries | Opt-in only |
| [`lazydocker.toml`](lazydocker.toml) | Native managers, AUR, Go | Opt-in only |
| [`lazygit.toml`](lazygit.toml) | Native managers, AUR, Go | Opt-in only |
| [`php-cs-fixer.toml`](php-cs-fixer.toml) | Composer | Opt-in only |
| [`pre-commit.toml`](pre-commit.toml) | pipx, uv | Opt-in only |
| [`prettier.toml`](prettier.toml) | npm, pnpm, Bun | Opt-in only |
| [`ripgrep.toml`](ripgrep.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`rubocop.toml`](rubocop.toml) | RubyGems | Opt-in only |
| [`ruff.toml`](ruff.toml) | pipx, uv | Opt-in only |
| [`shfmt.toml`](shfmt.toml) | Native managers, AUR, Go | Opt-in only |
| [`starship.toml`](starship.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`yq.toml`](yq.toml) | Native managers, AUR, Go | Opt-in only |
| [`zellij.toml`](zellij.toml) | Native managers, AUR, Cargo | Opt-in only |
| [`zoxide.toml`](zoxide.toml) | Native managers, AUR, Cargo | Opt-in only |

Together the library exercises every portable method family that can be
declared without platform-specific installer knowledge: `native`, `aur`,
`cargo`, `go`, `npm`, `pnpm`, `bun`, `pipx`, `uv`, `gem`, `composer`,
`flatpak`, `snap`, `appimage`, `github` and `http`. Recipes do not duplicate a
method label, and each names only the managers where the project is actually
published. Dedicated platform installers (`deb`, `msi`, `dmg`, `pkg`, `exe`,
`msix`, `appx`, `android`) remain project-specific because their identity is
tied to one host and one publisher. Methods that necessarily execute arbitrary
code (`git` with `build`), reference operator-local files (`local`) or a
machine-specific image (`container`) belong in a project schema or personal
manifest, never in a shared declarative recipe.

From a checkout of this repository, inspect the recipe and preview before
importing it into your user catalog:

```sh
depengine import recipes/nvim.toml --dry-run
depengine import recipes/nvim.toml
```

The original bytes are copied to your user catalog; future Git updates are
**not automatically applied**. To use the entire checked-out directory without
copying files, pass its **absolute** path when invoking a project command:

```sh
depengine validate --catalog "$(pwd)/recipes"
depengine install --catalog "$(pwd)/recipes"
```

A project still declares tools in `schema.toml`, e.g. `[tools.nvim]`.
`--catalog` replaces the default filesystem catalog roots for that invocation
and only looks up project-requested names (and `requires` dependencies). It
does not implicitly trust or import files. Use a pinned Git checkout when
reproducibility matters. `depengine import --expected-digest` can also verify a
digest obtained independently from a trusted source. The v2 lockfile records
canonical recipe digests and detects later changes.

Release-artifact recipes pin a literal SHA-256 and an explicit release tag.
When bumping one, update the tag, every asset URL/pattern and every checksum
together; `depengine import` rejects mutable `sha256:auto` entries.

## Managing depengine itself

The depengine executable is not a catalog recipe. Catalog recipes must stay
declarative, and `depengine import`/lookup reject command fields such as
`build`; a source build therefore cannot enter `recipes/*.toml`.

Self-update is driven by its own project-schema recipe instead:

```sh
depengine self update          # rebuild from the embedded recipe and swap atomically
depengine self update --check  # exit 1 when the running revision is behind
depengine self update --recipe recipes/depengine.toml.example --dry-run
```

[`depengine.toml.example`](depengine.toml.example) is the public copy of the
embedded self-update recipe. It is not importable and is deliberately not named
`*.toml`. It declares one `depengine` git method; the command resolves the
requested revision, expands `{version}` in the build command, verifies the new
executable and replaces the running binary atomically. `--repo`, `--ref`,
`--bin-dir` and `--recipe` override the source, revision, destination and
recipe. On Unix a failed build, install or verification leaves the previous
executable in place; Windows replacement remains manual.

## Contributing

- One portable file name, exactly one recipe, no private project-specific
  `requires`, optional parameters as normal TOML methods.
- Do not declare the same method label twice; prefer extending an existing
  method with `when`/distribution overrides before adding a near-duplicate
  block.
- All shared recipes must pass `depengine import <file> --dry-run`. Hooks,
  arbitrary-code plans, command fields and credential references are rejected.
- Downloaded artifacts require checksums. Shipped recipes use literal SHA-256
  hashes; `sha256:auto` remains accepted for operator-owned recipes with a
  trust-on-first-use warning. Importing checks syntax and policies, **not** live
  availability, signatures or publisher identity.
- Prefer portable manager aliases over claims that a project is published in
  package repositories where it is not. Include platform conditions.
- Test from the repository root with `go test ./internal/app ./tests/integration`
  and update the documentation when changing a bundled recipe. The test suite
  checks that each bundled file is byte-identical to its public equivalent, that
  every recipe stays declarative with literal checksums, and that the library
  still covers every portable method family.
