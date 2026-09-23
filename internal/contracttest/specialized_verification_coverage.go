package contracttest

func init() {
	RegisterCoverage(PhaseVerify, map[string]Coverage{
		"sdkman.pkg":     {Consumer: "SDKManAdapter.Observe", Rationale: "The candidate directory selected by pkg determines whether SDKMAN reports the installation present."},
		"sdkman.version": {Consumer: "SDKManAdapter.Observe", Rationale: "Exact SDKMAN version verification checks the requested version directory rather than the current symlink."},
		"asdf.pkg":       {Consumer: "AsdfAdapter.Observe", Rationale: "The asdf list query is scoped to the configured plugin name."},
		"asdf.version":   {Consumer: "AsdfAdapter.Observe", Rationale: "The listed version must match the exact configured asdf version."},
		"aur.pkg":        {Consumer: "AURAdapter.Observe", Rationale: "The helper's installed query selects the configured package name."},
		"pacstall.pkg":   {Consumer: "PacstallAdapter.Observe", Rationale: "The Pacstall inspection command selects the configured package name."},
		"yarn-berry.pkg": {Consumer: "YarnBerryAdapter.Observe", Rationale: "Local module resolution is queried for the configured package name."},
		"snap.pkg":       {Consumer: "BaseAdapter.Observe", Rationale: "Snap list verification selects the configured package name."},
		"go.pkg":         {Consumer: "GoAdapter.Observe", Rationale: "The configured import path determines the installed binary checked by Go verification."},
		"go.version":     {Consumer: "GoAdapter.Observe + GoAdapter.Check", Rationale: "Go verification requires the discovered binary version to match the requested version."},
	})
}
