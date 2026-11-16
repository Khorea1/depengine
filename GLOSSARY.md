# Dependency installation

depengine describes the tools a project needs, the eligible ways to install them, and the evidence needed to manage their installed state safely across hosts.

## Declarations

**Tool**:
A named project requirement that may be satisfied by an installed package or artifact, or by a group of other tools without its own installation.
_Avoid_: Package, when referring to the project requirement rather than a manager's package.

**Project schema**:
The project's declared tools and installation intent. Its explicit choices take precedence over personal and catalog definitions.
_Avoid_: Personal manifest.

**Personal manifest**:
An operator's installation recipes and defaults that can supplement a project schema without overriding its explicit choices.
_Avoid_: Project schema.

**Catalog**:
A user-owned collection of reusable recipes retrieved by tool name, rather than a list of project requirements.
_Avoid_: Project schema.

**Recipe**:
A reusable declaration of installation options for one named tool, supplied by the catalog when that tool is needed.
_Avoid_: Tool, when referring to the reusable declaration rather than the requirement.

**Tool dependency**:
Another named tool required before a tool can be satisfied; some dependencies apply only to a particular installation candidate.

**Dependency-only tool**:
A tool that is available as a prerequisite but is excluded from the normal set of installation roots unless explicitly selected.
_Avoid_: Virtual tool.

**Virtual tool**:
A named group of tool dependencies with no installation method of its own; satisfying it means satisfying its dependencies.
_Avoid_: Dependency-only tool.

**Profile**:
A selection of tools sharing a declared tag, used to limit which project requirements an operation targets.
_Avoid_: Installation scope.

## Installation selection

**Method**:
A kind of installation mechanism, such as a native package manager, an ecosystem manager, or an artifact download.
_Avoid_: Method candidate, when referring only to the mechanism's kind.

**Method candidate**:
One configured alternative for satisfying a tool, with its own identity and eligibility conditions. Candidates are considered in a defined order and may fall back to another eligible alternative.
_Avoid_: Method, when the distinction between alternatives matters.

**Package source**:
A repository, tap, or bucket made available to a package manager for a candidate; it is distinct from the downloadable artifact itself.
_Avoid_: Artifact URL.

**Installation scope**:
Who owns an installation, commonly the current user or the system; it does not select which tools belong to an operation.
_Avoid_: Profile.

## Identity and lifecycle

**Requested identity**:
The version, channel, tag, branch, or other selector that a declaration asks for, whether concrete or mutable.

**Resolved identity**:
The concrete version, revision, digest, or artifact chosen to satisfy a requested identity.

**Locked identity**:
An immutable resolved identity retained for reproducible operations where the installation method provides enough evidence to pin it.
_Avoid_: Requested identity.

**Observation**:
Read-only evidence of whether an installation target exists and which aspects of its identity can be established authoritatively.
_Avoid_: Recorded state.

**Verification state**:
The outcome of comparing a resolved target with an observation: satisfied, absent, drifted, unknown, or broken. Unknown means insufficient evidence; broken means invalid observation or verification, not proven absence.

**Drift**:
A proven mismatch between the desired resolved identity and an installed target's known identity, rather than mere uncertainty about the target.
_Avoid_: Unknown state.

**Resource ownership**:
Recorded evidence that depengine may manage or remove a resource it created or claimed, including resources shared by multiple tools. Presence alone does not establish ownership.
_Avoid_: Installed presence.
