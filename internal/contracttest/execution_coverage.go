package contracttest

import "github.com/Khorea1/depengine/internal/methodkind"

var executionProbeKinds = map[string]bool{
	"scoop": true, "choco": true,
	"cargo": true, "go": true, "pipx": true, "uv": true, "pip": true,
	"npm": true, "pnpm": true, "bun": true, "gem": true, "yarn": true,
	"yarn-berry": true, "composer": true, "apm": true, "vscode": true,
	"vscodium": true, "flatpak": true, "snap": true, "cask": true,
	"mas": true, "appman": true, "sdkman": true, "steamcmd": true,
	"pacstall": true, "aur": true, "conda": true, "asdf": true,
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
			entries[contract.Kind+"."+field] = Coverage{
				Consumer:  "TestExecuteEffectFieldProbes",
				Rationale: "differential FakeRunner probe asserts the install execution calls change when this exact field changes",
			}
		}
	}
	RegisterCoverage(PhaseExecute, entries)
}
