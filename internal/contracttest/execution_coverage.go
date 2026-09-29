package contracttest

import "github.com/Khorea1/depengine/internal/methodkind"

var executionProbeKinds = map[string]bool{
	"scoop": true, "choco": true,
	"cargo": true, "go": true, "pipx": true, "uv": true, "pip": true,
	"npm": true, "pnpm": true, "bun": true, "gem": true, "yarn": true,
	"yarn-berry": true, "composer": true, "apm": true, "vscode": true,
	"vscodium": true, "flatpak": true, "snap": true, "cask": true,
	"mas": true, "appman": true, "sdkman": true, "steamcmd": true,
	"pacstall": true, "aur": true, "conda": true, "asdf": true, "nix": true,
}

// executionProbeExclusions lists owned EffectExecute fields that the generic
// differential probe does not vary, mapped to the dedicated test that covers
// the effect instead. The probe asserts that changing the field changes the
// adapter's process calls through planning → resolve → install; a field whose
// effect only appears through a transport the FakeRunner probe cannot drive
// must instead be exercised by a focused runtime test.
var executionProbeExclusions = map[string]string{ // #nosec G101 -- contract field metadata contains "secret_ref"; no credential is embedded.
	"cargo.secret_ref": "internal/ecosystem/TestCargoGitSecretPrefetchesAndInstallsLocalCheckout",
}

func init() {
	entries := make(map[string]Coverage)
	for _, contract := range methodkind.Contracts {
		if !executionProbeKinds[contract.Kind] {
			continue
		}
		for field, spec := range contract.Fields {
			if spec.Effects&methodkind.EffectExecute == 0 {
				continue
			}
			key := contract.Kind + "." + field
			if _, excluded := executionProbeExclusions[key]; excluded {
				continue // dedicated runtime test covers the execute effect (see table)
			}
			entries[key] = Coverage{
				Consumer:  "TestExecuteEffectFieldProbes",
				Rationale: "differential FakeRunner probe asserts the install execution calls change when this exact field changes",
			}
		}
	}
	RegisterCoverage(PhaseExecute, entries)
}
