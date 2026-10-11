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
//   - base must not carry when/label/kind/cases-smuggling keys;
//   - expansion is total (all cases, every host) and order-preserving;
//   - no recursive inheritance, no inter-case priority, no new candidate kind.
package spike
