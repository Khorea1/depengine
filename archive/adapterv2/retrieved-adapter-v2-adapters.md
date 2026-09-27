# AdapterV2 research — B2: adapter inventory (2026-09-22)

Source: read-only scout of `feat/resolved-install-execution` tip. Feeds TODO-0 §2 migration order.
Interfaces: `internal/exec/adapter.go` — Adapter (:42), PlanResolver (:69), ResolvedInstaller (:83),
HostCompatibilityChecker (:97), Remover (:106), AvailabilityChecker (:135), ElevationRequirer (:151);
plus `exec.Versioner` (`internal/exec/state.go:16`, `InstalledVersion`).

## Adapter → interfaces → resolution entry points

Tier 0 — already V2-shaped (PlanResolver + ResolvedInstaller). Each still ships a legacy `Install()`
that re-resolves (double-resolution until strangler deletes it):

| # | Adapter | Type / file | Interfaces beyond Adapter | Resolution |
|---|---|---|---|---|
| 1 | native | `internal/exec:NativeAdapter`, `native_adapter.go:30` | Remover (:115,:132), AvailabilityChecker (:87) | NONE (clan :37, pkg :137) |
| 2 | N× per-manager | `NativeByManagerAdapter`, `native_adapter.go:204` | Remover, AvailabilityChecker, Versioner (winget-only :320) | NONE |
| 3 | scoop, choco | `winAdapter`, `win.go:35` | Remover only | NONE (pure argv) |
| 4 | 17× BaseAdapter | `internal/ecosystem:BaseAdapter`, `base.go:62` (pip, pipx, uv, npm, pnpm, bun, gem, yarn, composer, apm, flatpak, snap, vscode, vscodium, cask, appman, mas) | Remover iff RemoveTmpl (CanRemove :155; apm/vscode/vscodium/mas manual) | NONE (argv decoration in buildCmd :173) |
| 5 | cargo | `CargoAdapter`, `cargo.go:14` | Remover | NONE (local argv validation) |
| 6 | go | `GoAdapter`, `go.go:28` | Remover (custom GOBIN delete) | NONE (`@version` suffix) |
| 7–8 | aur, paru, yay | `AURAdapter` `aur.go:24`, `AURByNameAdapter` `aur_alias.go:16` | Remover (inherited) | NONE |
| 9–12 | sdkman, steamcmd, yarn-berry, pacstall | own types | Adapter ONLY | NONE |
| 13–14 | conda, asdf | own types | Remover | NONE (asdf `list` is presence query, not resolution) |
| 15 | container | `internal/container:ContainerAdapter`, `adapter.go:28` | Remover | LOCAL-ONLY string build (`containerRef` :44) |
| 16 | local | `internal/localartifactadapter:Adapter`, `adapter.go:21` | Remover | LOCAL-ONLY (`resolveCandidate` :69; Check+Install share it — single path) |
| 17 | git | `internal/git:GitAdapter`, `adapter.go:32` | PlanResolver (:209), ResolvedInstaller (:246), Remover | SINGLE shared `resolveCloneSource` :162 (`ghrelease.ResolveLatest` :192) used by both ResolvePlan :213 and legacy Install :234; `InstallResolved` :246 uses plan-only :261. Minor: `InstalledVersion` :93 touches `ghrelease.VersionTag` (read-only, keep out of install path) |
| 18 | http | `internal/httpdownload:HTTPAdapter`, `adapter.go:34` | PlanResolver (:39), ResolvedInstaller (:150), Remover, HostCompat (`compatibility.go:20`), ElevationRequirer (:68, structural, no assertion) | SINGLE shared `resolveDownloadPlan` :43 → `ResolveArtifactDetails` (`resolver.go:38`) with TWO mutually-exclusive branches: (A) url+ResolveLatest (:44), (B) repo+asset+ResolveAssetURL (:57); exclusivity enforced :41 |
| 19 | github | `GitHubAdapter`, `github_adapter.go:56` | PlanResolver (:58), ResolvedInstaller (:98), Remover, HostCompat (:28), ElevationRequirer (structural) | DELEGATED to shared `resolveDownloadPlan`; legacy Install :81 → `a.http.Install` + binary-default tweak |
| 20 | appimage | `AppImageAdapter`, `appimage_adapter.go:55` | PlanResolver (:59), ResolvedInstaller (:142), Remover, ElevationRequirer (structural) | DELEGATED; legacy Install :117 → http via `httpDelegate` :94; `InstallResolved` :142 → `http.InstallResolved` :153 |
| 21 | android | `AndroidAdapter`, `android_adapter.go:57` | PlanResolver (:61), ResolvedInstaller (:123); NO Remover (manual policy :145-151) | DELEGATED + termux-open dispatch |
| 22 | msi | `internal/msi:Adapter`, `adapter.go:30` | PlanResolver (:34), ResolvedInstaller (:70), Remover (:108) | DELEGATED (`ResolvePlan` :34 → `http.ResolvePlan`); fail-closed: http rejects .msi without `_allow_installer` (:168-183), msi sets it (:59,:86) |

NO adapter has more than one method-specific resolution path per candidate. Real hazard is COUPLING:
github/appimage/android/msi delegate transport to HTTPAdapter — http's resolver cannot change shape
without touching 4 adapters. Strangler must DELETE the legacy `Install()`s (git :230, http :139,
github :81, appimage :117, android :99, msi :46), not just add callers.
External `ghrelease` users (out of adapter scope): `internal/lock/lock.go:37,196` (lockfile pinning).

## Registration map

- Root: `main.go:initAdapters()` :53-68 — native :54 → native aliases :55 → `ecosystem.RegisterAll("paru")` :56 → git :57 → localartifact :58 → http :59 → github :60 → appimage :61 → android :62 → msi :63 → container :64 → `exec.WindowsAdapters()` :65-67.
- `exec.Register` (`registry.go:18`) panics on duplicate Kind; `exec.Replace` (:57) silent overwrite, only via `ecosystem.ReconfigureAUR` (:229).
- `RegisterNativeManagerAliases` (`native_adapter.go:172`): one `NativeByManagerAdapter` per name (dynamic count).
- `ecosystem.RegisterAll` (`registry.go:195`): Cargo + Go + 17× BaseAdapter (skips cargo/go) + AUR + paru/yay aliases + SDKMan + SteamCMD + YarnBerry + Pacstall + Conda + Asdf.
- Skew watch: `upgrade.go:149-151`, `install.go:165-167` build per-command adapter lists (git, http, container) — verify against global registry on current branch.
- `exec.WithAdapters()` (`executor.go:168-172`) is test-only on this branch; keep as the conformance-test injection seam (`internal/exectest/adapter.go:127`).

## Migration difficulty (easiest first)

1. sdkman, steamcmd, yarn-berry, pacstall (Adapter only) — trivial argv wrap.
2. apm, vscode, vscodium, mas (BaseAdapter, CanRemove=false) — same, no Remove path.
3. Other 13 BaseAdapter kinds — one shared migration covers all 17 (single class).
4. scoop, choco — Adapter+Remover+Versioner; risk is Windows test coverage, not complexity.
5. aur/paru/yay; 6. conda/asdf; 7. cargo/go (embed → BaseAdapter migration covers them except overrides).
8. native + N× per-manager — no resolution, but clan-detection + winget parsing; largest call-site surface.
9. container, local — local-only identity; need plan-threading through shared helpers.
10. git — already V2-shaped; delete legacy Install :230; keep `InstalledVersion` ghrelease out of install path.
11. http — full house + checksum/GPG/cache/extract; hardest single adapter; whole download family depends on it.
12. github/appimage/android/msi — thin delegations; migrate AFTER http's resolved-plan contract freezes. android needs a Remover-policy decision (currently none).
