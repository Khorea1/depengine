// Package spike implements the isolated A-vs-C prototype for issue dep-1n0.1.
//
// EXPERIMENTAL — not production code. It never changes the parser, merger,
// planner, adapters, lock, schema, or personal manifests. It reads a compact
// base+cases TOML representation (testdata/c_*.toml, which production must
// reject) and emits conventional schema TOML that goes through the existing
// ParseProjectSchema/MergeLayers path unchanged.
//
// Rules enforced here (spike contract):
//   - every case has an explicit, unique label and a when table;
//   - case overrides are limited to an allowlist:
//     asset, entrypoints, binary, strip_components, extract_to;
//   - the base must be the github method (table named "github" or carrying
//     kind="github"); any other table with cases is rejected;
//   - base must not carry when/label/kind;
//   - a case label must not collide with any other declared key of the tool;
//   - expansion is total (all cases, every host) and order-preserving;
//   - no recursive inheritance, no inter-case priority, no new candidate kind.
//
// Cross-layer semantics (by design, no magic): the macro is local per layer
// and consumes the base table, so conventional Kind+Label identity keeps
// working unchanged. An upper layer addresses expanded candidates by their
// labels (e.g. [packages.br.github-linux]); declaring the base name again
// (e.g. [packages.br.github]) appends a new candidate instead of updating
// the expanded ones. Proven by TestCrossLayerBaseOverride.
package spike
