# Format v1 freeze gate

Schema, lock, and state format version `1` remain pre-freeze until the following
project criteria are satisfied:

- the adversarial cross-platform manifest fixture matrix is expanded;
- method contract and conformance suites cover validation, identity, dry-run,
  version, source, scope, environment, lock, lifecycle, idempotency, errors, and
  secret redaction;
- planner fuzz/property tests and lifecycle/state invariant tests exist;
- product claims and public documentation are aligned with guarantees actually
  enforced by the implementation.

Completing these criteria permits the project to run the v1 freeze review. The
freeze itself is a deliberate project decision; satisfying the checklist does not
automatically change the compatibility promise. At that review, lock-envelope v1
read support must receive an explicit disposition separate from schema/state
format freeze. A decision to stop accepting v1 lock inputs is effective only for
a designated incompatible release after its deprecation window, documented in
`compatibility.md`, has ended; freeze review completion alone does not remove
`ResolveLegacyV1`, `ApplyLegacyV1`, or v1 frozen validation.
