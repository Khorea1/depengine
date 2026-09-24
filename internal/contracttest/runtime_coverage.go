package contracttest

func init() {
	RegisterCoverage(PhaseResolveRuntime, map[string]Coverage{
		"native.pkg_overrides": {Consumer: "TestNativePackageOverrideAcrossRuntimeBoundaries", Rationale: "clan-specific package identity is selected before adapter execution"},
		"git.url":              {Consumer: "internal/git/adapter_test.go", Rationale: "clone source is consumed by runtime source resolution"},
		"http.release":         {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "release selector changes the resolved artifact URL and version"},
		"http.branch":          {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "branch selector changes the resolved artifact URL and version"},
		"github.release":       {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "release selector changes the resolved artifact URL and version"},
		"github.branch":        {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "branch selector changes the resolved artifact URL and version"},
		"github.secret_ref":    {Consumer: "internal/exec/TestResolveCandidatePlanPassesResolvedGitHubSecretContext", Rationale: "the canonical resolver resolves the typed GitHub reference before release API access"},
		"appimage.release":     {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "release selector changes the resolved artifact URL and version"},
		"appimage.branch":      {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "branch selector changes the resolved artifact URL and version"},
		"android.release":      {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "release selector changes the resolved artifact URL and version"},
		"android.branch":       {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "branch selector changes the resolved artifact URL and version"},
		"msi.release":          {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "release selector changes the resolved artifact URL and version"},
		"msi.branch":           {Consumer: "TestRuntimeReleaseAndBranchResolution", Rationale: "branch selector changes the resolved artifact URL and version"},
	})
	RegisterCoverage(PhaseExecute, map[string]Coverage{
		"github.secret_ref":     {Consumer: "internal/exec/TestInstallResolvedCandidatePassesResolvedGitHubSecretContext", Rationale: "the canonical resolved-plan installer re-resolves and transports the typed token for asset execution"},
		"native.pkg":            {Consumer: "internal/exec/TestNativeAdapterV2PackageFieldChangesExecuteAndVerify", Rationale: "changing the package changes the native install command"},
		"native.pkg_overrides":  {Consumer: "TestNativePackageOverrideAcrossRuntimeBoundaries", Rationale: "serial and batch installs use the selected clan package"},
		"winget.pkg":            {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing package identity changes the winget install command"},
		"winget.version":        {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing version changes the winget install command"},
		"winget.source":         {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing source changes the winget install command"},
		"winget.scope":          {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing scope changes the winget install command"},
		"winget.architecture":   {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing architecture changes the winget install command"},
		"winget.installer_type": {Consumer: "internal/exec/TestNativeByManagerWingetV2ExecuteFieldsChangeCommand", Rationale: "changing installer type changes the winget install command"},
		"git.url":               {Consumer: "internal/git/resolved_install_test.go", Rationale: "resolved clone executes the planned source"},
		"git.artifact":          {Consumer: "internal/git/adapter_test.go", Rationale: "artifact selects the file copied from the temporary clone"},
		"git.managed_paths":     {Consumer: "internal/git/adapter_test.go", Rationale: "managed paths determine owned payloads for install and removal"},
		"local.local_path":      {Consumer: "internal/localartifactadapter/adapter_v2_test.go", Rationale: "project-relative source determines installed bytes"},
		"local.checksum":        {Consumer: "internal/localartifactadapter/adapter_v2_test.go", Rationale: "checksum gates materialization of local bytes"},
		"local.install_dir":     {Consumer: "internal/localartifactadapter/adapter_test.go", Rationale: "install directory selects the owned destination"},
		"container.manager":     {Consumer: "internal/container/adapter_test.go", Rationale: "manager selects the runtime command and image store"},
		"container.source":      {Consumer: "internal/container/adapter_test.go", Rationale: "source participates in the pulled image reference"},
		"container.tag":         {Consumer: "internal/container/adapter_test.go", Rationale: "tag participates in the pulled image reference"},
		"container.digest":      {Consumer: "internal/container/adapter_test.go", Rationale: "digest pins the pulled image content identity"},
		"container.platform":    {Consumer: "internal/container/adapter_test.go", Rationale: "platform selects and verifies the requested image variant"},
		"msi.product_name":      {Consumer: "internal/msi/adapter_v2_test.go", Rationale: "product name selects the product passed to msiexec"},
		"msi.publisher":         {Consumer: "internal/msi/adapter_v2_test.go", Rationale: "publisher narrows product lookup before install or removal"},
	})
	RegisterCoverage(PhaseVerify, map[string]Coverage{
		"native.pkg":           {Consumer: "internal/exec/TestNativeAdapterV2PackageFieldChangesExecuteAndVerify", Rationale: "changing the package changes the native verification query and observed identity"},
		"native.pkg_overrides": {Consumer: "TestNativePackageOverrideAcrossRuntimeBoundaries", Rationale: "observe queries the clan-specific package identity"},
		"winget.pkg":           {Consumer: "internal/exec/TestNativeByManagerWingetV2VerifyFieldsChangeObservation", Rationale: "changing package identity changes the winget verification query and observed identity"},
		"winget.version":       {Consumer: "internal/exec/TestNativeByManagerWingetV2VerifyFieldsChangeObservation", Rationale: "changing version changes reconciliation against controlled winget state"},
		"winget.source":        {Consumer: "internal/exec/TestNativeByManagerWingetV2VerifyFieldsChangeObservation", Rationale: "changing source changes the winget query and reconciliation against controlled state"},
		"git.managed_paths":    {Consumer: "internal/git/adapter_test.go", Rationale: "observe and remove are bounded by declared owned paths"},
		"local.checksum":       {Consumer: "internal/localartifactadapter/adapter_v2_test.go", Rationale: "observation reports absence when installed content drifts"},
		"local.install_dir":    {Consumer: "internal/localartifactadapter/adapter_test.go", Rationale: "observation checks the configured destination"},
		"appimage.install_dir": {Consumer: "internal/httpdownload/appimage_adapter_test.go", Rationale: "observation checks the configured stable binary location"},
		"container.manager":    {Consumer: "internal/container/adapter_test.go", Rationale: "observation queries the selected runtime's image store"},
		"container.source":     {Consumer: "internal/container/adapter_test.go", Rationale: "observation queries the requested image source"},
		"container.tag":        {Consumer: "internal/container/adapter_test.go", Rationale: "observation distinguishes the requested image tag"},
		"container.digest":     {Consumer: "internal/container/adapter_test.go", Rationale: "observation verifies immutable image identity"},
		"container.platform":   {Consumer: "internal/container/adapter_test.go", Rationale: "observation compares the installed image platform"},
		"msi.product_name":     {Consumer: "internal/msi/adapter_v2_test.go", Rationale: "verification looks up the configured product name"},
		"msi.publisher":        {Consumer: "internal/msi/adapter_v2_test.go", Rationale: "verification looks up the configured publisher"},
	})
	registerRuntimeFields(PhaseResolveRuntime, "http", "internal/httpdownload/resolved_install_test.go", "runtime source resolution supplies the concrete artifact", "url", "repo", "asset")
	registerRuntimeFields(PhaseResolveRuntime, "http", "internal/planner/resolve_effect_test.go", "static planning projects typed secret references into the candidate plan without resolving their values", "secret_ref", "checksum_secret_ref", "signature_secret_ref")
	registerRuntimeFields(PhaseResolveRuntime, "github", "TestRuntimeReleaseAndBranchResolution", "runtime source resolution supplies the concrete artifact", "repo", "asset")
	registerRuntimeFields(PhaseResolveRuntime, "appimage", "TestRuntimeReleaseAndBranchResolution", "runtime source resolution supplies the concrete artifact", "url", "repo", "asset")
	registerRuntimeFields(PhaseResolveRuntime, "android", "TestRuntimeReleaseAndBranchResolution", "runtime source resolution supplies the concrete artifact", "url", "repo", "asset")
	registerRuntimeFields(PhaseResolveRuntime, "msi", "TestRuntimeReleaseAndBranchResolution", "runtime source resolution supplies the concrete artifact", "url", "repo", "asset")
	registerRuntimeFields(PhaseResolveRuntime, "local", "internal/localartifactadapter/adapter_v2_test.go", "project-relative source and checksum resolve to local content identity", "local_path", "checksum")

	common := []string{"checksum", "checksum_url", "checksum_file_format", "signature_url", "signing_key"}
	registerRuntimeFields(PhaseExecute, "http", "internal/httpdownload/resolved_install_test.go", "resolved metadata is consumed by the shared HTTP installer", append(common, "url", "repo", "asset", "extract_to", "binary", "entrypoints", "link_dir", "sudo_required", "strip_components", "scope")...)
	registerRuntimeFields(PhaseExecute, "http", "internal/exec/http_secret_test.go", "a reached HTTP candidate resolves typed references and hands request-scoped credentials to the runtime-only Bearer transport", "secret_ref", "checksum_secret_ref", "signature_secret_ref")
	registerRuntimeFields(PhaseExecute, "github", "internal/httpdownload/github_v2_test.go", "GitHub delegates artifact execution to the shared HTTP installer", append(common, "repo", "asset", "extract_to", "binary", "entrypoints", "link_dir", "sudo_required", "strip_components", "scope")...)
	registerRuntimeFields(PhaseExecute, "appimage", "internal/httpdownload/appimage_adapter_test.go", "AppImage delegates download and archive execution to the shared HTTP installer", append(common, "url", "repo", "asset", "binary", "entrypoints", "link_dir", "sudo_required", "strip_components", "install_dir", "desktop", "scope")...)
	registerRuntimeFields(PhaseExecute, "android", "internal/httpdownload/android_adapter_test.go", "Android delegates download and integrity checks to the shared HTTP installer", append(common, "url", "repo", "asset", "sudo_required")...)
	registerRuntimeFields(PhaseExecute, "msi", "internal/msi/adapter_v2_test.go", "MSI delegates artifact integrity and source handling to HTTP before msiexec", append(common, "url", "repo", "asset")...)
	registerRuntimeFields(PhaseExecute, "git", "internal/git/adapter_test.go", "Git execution consumes clone, build, ownership, and placement settings", "branch", "tag", "rev", "depth", "submodules", "build", "extract_to", "binary")

	registerRuntimeFields(PhaseVerify, "http", "internal/httpdownload/http_v2_test.go", "observation checks configured payload and launcher locations", "extract_to", "binary", "entrypoints", "link_dir", "scope")
	registerRuntimeFields(PhaseVerify, "github", "internal/httpdownload/github_v2_test.go", "GitHub observation delegates configured payload checks to HTTP", "extract_to", "binary", "entrypoints", "link_dir", "scope")
	registerRuntimeFields(PhaseVerify, "appimage", "internal/httpdownload/appimage_v2_test.go", "observation checks the configured stable payload and launcher locations", "binary", "entrypoints", "link_dir", "scope")
	registerRuntimeFields(PhaseVerify, "git", "internal/git/conformance_test.go", "observation checks the configured owned paths and install destination", "extract_to", "binary")
}

func registerRuntimeFields(phase Phase, kind, consumer, rationale string, fields ...string) {
	entries := make(map[string]Coverage, len(fields))
	for _, field := range fields {
		entries[kind+"."+field] = Coverage{Consumer: consumer, Rationale: rationale}
	}
	RegisterCoverage(phase, entries)
}
