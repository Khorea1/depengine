# Plan 0001: Expand `when` Conditions and Bucket Expansion in `method_order`

**Status:** Approved  
**Date:** 2026-07-24  
**Authors:** SchemaArchitect, ExecutorEngineer, SafetyReviewer (orchestrated)  
**Auditor:** Main (final approval)

---

## Problems

### Problem A — No Bucket Expansion in `method_order` / `method_prefer`

`method_order = ["native", "cargo"]` works because `native` is a pseudo-bucket that
resolves to the distro's package manager. But `method_order = ["python", "native"]`
does **not** work — `"python"` is not expanded to `["pip", "pipx", "uv"]`.

`DefaultBuckets` exists (`schema.go:39`) and is used in `buildMethods` for tool-level
`python = true`, but `ExpandMethodOrder` and `Validate` ignore it.

### Problem B — `when` Conditions Only Support `distro_family`

`Condition` has only `DistroFamily []string`. `parseCondition` silently drops unknown
keys. `detect_os.sh` captures 13+ fields (`libc`, `arch`, `init_system`, `os`,
`is_container`, `is_wsl`, `kernel`, `distro_id`, …), all stored in `engine.Facts`,
but only `distro_family` is usable in `when`.

### Problem C — `native` Has Privileged Expansion vs Buckets

`native` is expanded by hardcoded logic in `ExpandMethodOrder` + `IsNativeManagerName`.
Buckets defined in `DefaultBuckets` are not consulted in the same flow, creating an
asymmetry where `python` (a conceptual bucket) and `native` (a conceptual bucket)
behave differently.

---

## Design

### 1. `Condition` Struct — New Fields

```go
type Condition struct {
    DistroFamily []string // existing — OR-match against resolved clan
    DistroID     []string // new — exact OR-match against Facts.DistroID
    Arch         []string // new — exact OR-match against Facts.TargetArch
    OS           []string // new — exact OR-match against Facts.OS
    Kernel       []string // new — exact OR-match against Facts.Kernel
    Libc         []string // new — PREFIX OR-match against Facts.Libc (§1a)
    InitSystem   []string // new — exact OR-match against Facts.InitSystem
    IsWSL        *bool    // new — three-state: nil=ignore, &true=yes, &false=no
    IsContainer  *bool    // new — three-state: nil=ignore, &true=yes, &false=no
}
```

**Rationale:**

| Type | Fields | Why |
|------|--------|-----|
| `[]string` | DistroFamily, DistroID, Arch, OS, Kernel, InitSystem | Multiple acceptable values (e.g. `arch = ["x86_64", "aarch64"]`) |
| `[]string` | Libc | Prefix match because detect_os.sh emits `"glibc 2.35"` — `libc = ["glibc"]` should match |
| `*bool` | IsWSL, IsContainer | Three-state: nil = ignore, true = must be true, false = must be false. `bool` would conflate "not specified" with "must be false" |

**Zero-value semantics:**

- `nil` or empty `[]string` → field not applied (always matches)
- `nil` `*bool` → field not applied (always matches)

**`IsZero()` — comprehensive check for empty Condition:**

```go
func (c *Condition) IsZero() bool {
    return len(c.DistroFamily) == 0 &&
        len(c.DistroID) == 0 &&
        len(c.Arch) == 0 &&
        len(c.OS) == 0 &&
        len(c.Kernel) == 0 &&
        len(c.Libc) == 0 &&
        len(c.InitSystem) == 0 &&
        c.IsWSL == nil &&
        c.IsContainer == nil
}
```

#### 1a. Libc Prefix Match — Rationale

`detect_os.sh` emits version-annotated libc values:

```
"glibc 2.35"
"musl 1.2.4"
```

A condition `when = { libc = ["glibc"] }` should match regardless of minor version.
Exact match would force users to write `libc = ["glibc 2.35"]` which varies by distro.

**Decision:** Case-insensitive `strings.HasPrefix` for Libc only. All other string
fields use `strings.EqualFold` (case-insensitive exact match).

---

### 2. `parseCondition` — New Keys

```go
func parseCondition(raw any) *Condition {
    rm, ok := raw.(map[string]any)
    if !ok { return nil }
    cond := &Condition{}
    var invalidKeys []string
    for k, v := range rm {
        switch k {
        case "distro_family": cond.DistroFamily = toStringSlice(v)
        case "distro_id":     cond.DistroID     = toStringSlice(v)
        case "arch":          cond.Arch         = toStringSlice(v)
        case "os":            cond.OS           = toStringSlice(v)
        case "kernel":        cond.Kernel       = toStringSlice(v)
        case "libc":          cond.Libc         = toStringSlice(v)
        case "init_system":   cond.InitSystem   = toStringSlice(v)
        case "is_wsl":        cond.IsWSL        = parseBoolPtr(v)
        case "is_container":  cond.IsContainer  = parseBoolPtr(v)
        default:
            invalidKeys = append(invalidKeys, k)
        }
    }
    if len(invalidKeys) > 0 {
        log.Default.Debug("parseCondition: ignoring unknown keys", "keys", invalidKeys)
    }
    if cond.IsZero() {
        return nil
    }
    return cond
}
```

**Unknown key policy:** Log at debug, retain current behavior (silent skip).
Rationale: future schema versions may add new fields. Hard-erroring would make
older depengine binaries reject newer schema files.

**Helpers:**

```go
func toStringSlice(v any) []string {
    switch t := v.(type) {
    case []any:  return anySliceToStrings(t)
    case string: return []string{t}  // single-value sugar
    default:     return nil
    }
}

func parseBoolPtr(v any) *bool {
    b, ok := v.(bool)
    if !ok { return nil }
    return &b
}
```

---

### 3. `Condition.Match(facts *engine.Facts) bool`

Location: `pkg/schema/schema.go` (method on `*Condition`). Safe because
`pkg/schema` already imports `pkg/engine` via `placeholder.go`.

```go
// Match reports whether this condition is satisfied by the given system facts.
// All non-zero fields must match (AND semantics). Empty/nil fields are skipped.
// Returns true for a nil receiver (nil condition = always match).
func (c *Condition) Match(facts *engine.Facts) bool {
    if c == nil { return true }
    if facts == nil {
        // Conservative: can't verify without facts. A condition with only
        // DistroFamily matches if no other fields are set (empty condition).
        return c.IsZero()
    }

    // DistroFamily: resolved clan (ResolveFamily is a pure function)
    if len(c.DistroFamily) > 0 {
        clan := engine.ResolveFamily(facts)
        if !engine.MatchesDistroFamily(clan, c.DistroFamily) {
            return false
        }
    }

    // String-slice fields: case-insensitive OR-match (exact)
    if len(c.DistroID) > 0   && !matchExact(c.DistroID, facts.DistroID)     { return false }
    if len(c.Arch) > 0       && !matchExact(c.Arch, facts.TargetArch)       { return false }
    if len(c.OS) > 0         && !matchExact(c.OS, facts.OS)                 { return false }
    if len(c.Kernel) > 0     && !matchExact(c.Kernel, facts.Kernel)         { return false }
    if len(c.InitSystem) > 0 && !matchExact(c.InitSystem, facts.InitSystem) { return false }

    // Libc: prefix match for version-suffixed values
    if len(c.Libc) > 0 && !matchPrefix(c.Libc, facts.Libc) { return false }

    // Three-state bools
    if c.IsWSL != nil       && *c.IsWSL != facts.IsWSL           { return false }
    if c.IsContainer != nil && *c.IsContainer != facts.IsContainer { return false }

    return true
}

func matchExact(allowed []string, actual string) bool {
    for _, a := range allowed {
        if strings.EqualFold(a, actual) { return true }
    }
    return false
}

func matchPrefix(allowed []string, actual string) bool {
    actualLower := strings.ToLower(actual)
    for _, a := range allowed {
        if strings.HasPrefix(actualLower, strings.ToLower(a)) { return true }
    }
    return false
}
```

---

### 4. Bucket Expansion in `MethodOrder` / `MethodPrefer` / `MethodOnly`

#### `ExpandBuckets` — New Function

```go
var DefaultBuckets = map[string][]string{
    "python": {"pip", "pipx", "uv"},
    "node":   {"npm", "pnpm", "bun"},
}

func ExpandBuckets(order []string) []string {
    var expanded []string
    for _, k := range order {
        if methods, ok := DefaultBuckets[k]; ok {
            for _, m := range methods {
                if !contains(expanded, m) {
                    expanded = append(expanded, m)
                }
            }
        } else {
            expanded = append(expanded, k)
        }
    }
    return expanded
}

func contains(slice []string, s string) bool {
    for _, v := range slice {
        if v == s { return true }
    }
    return false
}
```

#### Integration in `EffectiveMethodOrder`

**Current flow:**

```
EffectiveMethodOrder(tool, defaultOrder, nativeManagerName)
  → determine toolList and defList
  → ExpandMethodOrder(toolList, nativeManagerName)   // native expansion
  → ExpandMethodOrder(defList, nativeManagerName)    // native expansion
  → MergeMethodOrder(toolList, defList)               // for prefer/order
```

**New flow:**

```
EffectiveMethodOrder(tool, defaultOrder, nativeManagerName)
  → determine toolList and defList
  → ExpandBuckets(toolList)                            // NEW: bucket → concrete
  → ExpandBuckets(defList)                             // NEW: bucket → concrete
  → ExpandMethodOrder(toolList, nativeManagerName)    // native expansion
  → ExpandMethodOrder(defList, nativeManagerName)     // native expansion
  → MergeMethodOrder(toolList, defList)
```

**Why ExpandBuckets before native expansion:**

1. Bucket expansion turns `"python"` → `["pip", "pipx", "uv"]` (concrete kinds)
2. Native expansion handles these concrete kinds (no bucket name clashes with
   native manager names)
3. If a bucket ever contained `"native"` as a method, it would be expanded
   first and then native-expanded correctly

#### Example Expansion Traces

| Input | Output (Arch) | Output (Debian) |
|-------|---------------|-----------------|
| `["python", "native"]` | `["pip","pipx","uv","native"]` | `["pip","pipx","uv","native"]` |
| `["native", "python"]` | `["native","pip","pipx","uv"]` | `["native","pip","pipx","uv"]` |
| `["cargo", "python"]` | `["cargo","pip","pipx","uv"]` | `["cargo","pip","pipx","uv"]` |
| `["python", "apt", "cargo"]` | `["pip","pipx","uv","native","cargo"]` | `["pip","pipx","uv","native","cargo"]` |

Note: `"apt"` is a native manager name → maps to `"native"` via
`ExpandMethodOrder`. On Arch, `"apt"` would not match `pacman` → stays as
`"apt"` (no adapter registered for it on Arch → skipped).

---

### 5. Validate — Accept Bucket Names

#### `defaults.method_order` Validation

```go
for _, kind := range s.Defaults.MethodOrder {
    if native.IsNativeManagerName(kind) { continue }
    if _, isBucket := DefaultBuckets[kind]; isBucket {    // NEW
        warnings = append(warnings, fmt.Sprintf(           // NEW: informative warning
            "defaults.method_order entry %q is a bucket name "+
            "→ expands to %v", kind, DefaultBuckets[kind]))
        continue
    }
    if _, ok := set[kind]; !ok {
        warnings = append(warnings, ...)
    }
}
```

#### Per-tool `MethodOrder` / `MethodPrefer` / `MethodOnly` Validation

```go
for _, kind := range tool.MethodOrder {  // and MethodPrefer, MethodOnly
    if native.IsNativeManagerName(kind) { continue }
    if _, isBucket := DefaultBuckets[kind]; isBucket { continue }  // NEW
    if _, ok := set[kind]; !ok {
        hardErrors = append(hardErrors, ...)
    }
}
```

**Also:** Validate `MethodPrefer` and `MethodOnly` — these were previously
**not validated at all**, allowing invalid entries to pass silently until
runtime. After this change, invalid entries produce a hard error. This is
an acceptable breaking change for previously-underspecified fields.
Document in changelog.

---

### 6. Executor Integration

#### Store Facts on Executor

```go
type Executor struct {
    clan               string
    facts              *engine.Facts     // NEW — system facts for when-condition eval
    rn                 run.Runner
    // ... existing fields ...
}
```

#### `WithFacts` Option

```go
func WithFacts(f *engine.Facts) Option {
    return func(ex *Executor) { ex.facts = f }
}
```

#### Replace When-Condition Check in `tryMethods`

**Current:**

```go
if method.When != nil && len(method.When.DistroFamily) > 0 {
    if !engine.MatchesDistroFamily(ex.clan, method.When.DistroFamily) {
        attempt.Status = "skip_when"
        continue
    }
}
```

**New** — single gate delegates to `Condition.Match`:

```go
if method.When != nil && !method.When.Match(ex.facts) {
    attempt.Status = "skip_when"
    continue
}
```

#### Same Replacement in `ExplainTool`

Same pattern: replace the `DistroFamily`-only check with `method.When.Match(ex.facts)`.

#### Update Callers

```go
// install.go — already has facts
ex := exec.New()
exec.WithFacts(facts)(ex)    // NEW
report, err := ex.Execute(ctx, s, clan)

// graph_why.go — already has facts
ex := exec.New()
exec.WithRunner(run.OSExecRunner{})(ex)
exec.WithFacts(facts)(ex)    // NEW
attempts := ex.ExplainTool(ctx, tool, clan)
```

---

### 7. Validation Fix in `validateWhenDirectives`

**Problem:** `validateWhenDirectives` in `pkg/validate/structural.go` checks
`len(mc.When.DistroFamily) == 0` to detect empty when clauses. After our
change, a `when = { arch = ["x86_64"] }` has `DistroFamily` empty but `Arch`
set — the old check would flag it as `WarnUnknownWhenKey`.

**Fix:** Change the emptiness check to:

```go
if mc.When != nil && mc.When.IsZero() {
    r.Add(ValidationError{...})
}
```

---

## Files Changed

| File | Change |
|------|--------|
| `pkg/schema/schema.go` | `Condition` struct (+7 fields), `IsZero()`, `parseCondition` (+7 cases), `toStringSlice`, `parseBoolPtr`, `Condition.Match`, `matchExact`, `matchPrefix`, `ExpandBuckets`, `contains`, `EffectiveMethodOrder` (bucket expansion), `Validate` (bucket acceptance + MethodPrefer/MethodOnly validation) |
| `pkg/schema/schema_test.go` | 13 new tests |
| `pkg/exec/executor.go` | `Executor.facts` field, `WithFacts` option, both when-check replacements |
| `pkg/exec/executor_test.go` | Existing tests preserved via nil-facts guard |
| `install.go` | `WithFacts(facts)` before `Execute` |
| `graph_why.go` | `WithFacts(facts)` before `ExplainTool` |
| `pkg/validate/structural.go` | `validateWhenDirectives` emptiness check → `IsZero()` |

---

## Test Checklist

| # | Test | What It Covers |
|---|------|----------------|
| 1 | `TestConditionMatches` | Full matrix of all condition fields vs various Facts configs |
| 2 | `TestConditionMatchesNilFacts` | Fallback behavior with nil facts |
| 3 | `TestConditionMatchesPartialFacts` | Partial Facts (Go runtime fallback) |
| 4 | `TestConditionIsZero` | Proves every new field is checked in IsZero |
| 5 | `TestExpandBucketsNoOp` | Order with no bucket names returns unchanged |
| 6 | `TestExpandBucketsExpansion` | Order with `"python"` → `["pip","pipx","uv"]` |
| 7 | `TestExpandBucketsDeduplicate` | `"python" + "pip"` → `["pip","pipx","uv"]` (no dupe) |
| 8 | `TestEffectiveMethodOrderWithBuckets` | Full pipeline with bucket names |
| 9 | `TestValidateAcceptsBucketNames` | Bucket names in method_prefer/order/only pass validation |
| 10 | `TestValidateRejectsUnknownKindInMethodPrefer` | `method_prefer = ["nonexistent"]` is now hard error |
| 11 | `TestParseConditionNewFields` | Each new field parses correctly |
| 12 | `TestParseConditionAllFields` | Multiple fields together |
| 13 | `TestParseConditionUnknownKeysStillIgnored` | Unknown keys still don't error (backward compat) |

---

## Implementation Order

### Phase 1: Condition Expansion (schema)
1. Add new fields to `Condition` struct
2. Add `IsZero()` method
3. Add `toStringSlice`, `parseBoolPtr` helpers
4. Update `parseCondition` with new case labels
5. Add `Condition.Match(facts *engine.Facts) bool`
6. Add `matchExact`, `matchPrefix` helpers

### Phase 2: Bucket Expansion (schema)
7. Add `ExpandBuckets` and `contains` helper
8. Update `EffectiveMethodOrder` to call `ExpandBuckets` before native expansion
9. Update `Validate` to accept bucket names + validate MethodPrefer/MethodOnly

### Phase 3: Executor Integration (exec + callers)
10. Add `facts *engine.Facts` field to `Executor` struct
11. Add `WithFacts` option constructor
12. Replace `DistroFamily`-only check in `tryMethods` with `method.When.Match(ex.facts)`
13. Same replacement in `ExplainTool`
14. Add `WithFacts(facts)` to `install.go` and `graph_why.go`

### Phase 4: Validation Fix (validate)
15. Update `validateWhenDirectives` emptiness check to use `IsZero()`
16. Update warning message for bucket names in `defaults.method_order`

### Phase 5: Testing
17. Write all 13 tests from checklist
18. Run full test suite
19. Manual smoke test with a real schema

---

## Backward Compatibility

| Scenario | Before | After | Risk |
|----------|--------|-------|------|
| `when` absent entirely | ✅ always matches | ✅ `Match(nil, _)` → true | None |
| `when = { distro_family = ["arch"] }` | ✅ works | ✅ identical | None |
| `when = {}` | ✅ all-zero → nil | ✅ `IsZero()` → nil | None |
| `when = { unknown = "x" }` | ⚠️ debug log, ignored | ✅ same behavior (debug log, ignored) | None |
| `method_order = ["native","cargo"]` | ✅ works | ✅ identical (no bucket names) | None |
| `method_order = ["python"]` (in `defaults`) | ⚠️ warning, no-op at runtime | ✅ expands to pip/pipx/uv | **Intended new behavior** — warning says "expands to [pip pipx uv]" |
| `method_prefer = ["nonexistent"]` | ⚠️ silently ignored | ❌ hard error at validation | Acceptable — was a bug to silently accept |
| `when = { distro_family = ["arch"], libc = ["musl"] }` | ⚠️ `libc` debug-logged, ignored | ✅ `libc` evaluated | Only affects schemas that had non-functional `when` clauses |

**Verdictpkg:** Zero breakage for valid existing schemas. Two behavioral changes
(intended, documented in changelog): bucket activation and MethodPrefer/Only
validation hardening.

---

## Examples of New Capabilities

```toml
# method_order with bucket names
[defaults]
method_order = ["native", "python", "cargo"]

# when with architecture + libc
[tools.alpine-bin]
  [tools.alpine-bin.http]
  url = "https://dl-cdn.alpinelinux.org/alpine/{latest}/main/{arch}/pkg-{latest}.apk"
  when = { libc = ["musl"], arch = ["x86_64", "aarch64"] }

# when container-aware
[tools.clamav]
  [tools.clamav.native]
  pkg  = "clamav"
  when = { is_container = false }

# when distro_id-specific (not just distro_family)
[tools.podman]
  [tools.podman.native]
  pkg  = "podman"
  when = { distro_id = ["ubuntu", "debian"] }

# method_prefer with bucket + when cross-field
[tools.neovim]
method_prefer = ["python", "native"]
  [tools.neovim.pip]
  pkg  = "neovim"
  when = { is_wsl = true, distro_family = ["debian"] }

# void-musl specific package name (the original use case)
[tools.git]
  [tools.git.xbps-musl]
  pkg  = "git-musl-git"
  when = { distro_id = ["void"], libc = ["musl"] }

  [tools.git.xbps]
  pkg  = "git"
  when = { distro_id = ["void"] }
```
