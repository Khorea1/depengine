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
automatically change the compatibility promise.
