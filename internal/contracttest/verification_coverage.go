package contracttest

func init() {
	RegisterCoverage(PhaseVerify, map[string]Coverage{
		"cargo.pkg":         {Consumer: "CargoAdapter.Observe", Rationale: "A/B crate names select independently present cargo install entries."},
		"cargo.version":     {Consumer: "CargoAdapter.Observe + plan.Reconcile", Rationale: "The installed version is authoritative and a different requested version reconciles as drift."},
		"cargo.bins":        {Consumer: "CargoAdapter.Observe", Rationale: "Changing the required binary set changes whether the observed install entry is present."},
		"cargo.root":        {Consumer: "CargoAdapter.Observe", Rationale: "The install-list query is scoped to the selected root and observes independent root state."},
		"conda.pkg":         {Consumer: "CondaAdapter.Observe", Rationale: "A/B package names select independently present conda records."},
		"conda.version":     {Consumer: "CondaAdapter.Observe + plan.Reconcile", Rationale: "The observed conda version is authoritative and reconciles against the requested version."},
		"conda.build":       {Consumer: "CondaAdapter.Observe + plan.Reconcile", Rationale: "The observed conda build is authoritative revision identity and differing builds drift."},
		"conda.environment": {Consumer: "CondaAdapter.Observe", Rationale: "The conda list query targets the configured named environment."},
		"conda.prefix":      {Consumer: "CondaAdapter.Observe", Rationale: "The conda list query targets the configured prefix."},
		"pip.pkg":           {Consumer: "BaseAdapter.Observe", Rationale: "The package query selects independently present pip package state."},
		"pip.version":       {Consumer: "BaseAdapter.Observe + plan.Reconcile", Rationale: "Pip verification compares the observed installed version with the requested version."},
		"pipx.scope":        {Consumer: "BaseAdapter.Observe", Rationale: "Pipx verification queries local and global environments independently."},
		"flatpak.pkg":       {Consumer: "BaseAdapter.Observe", Rationale: "A/B app refs select independently installed Flatpak state."},
		"flatpak.branch":    {Consumer: "BaseAdapter.Observe", Rationale: "The selected branch is part of the ref queried during verification."},
		"flatpak.remote":    {Consumer: "BaseAdapter.Observe", Rationale: "Verification compares the queried installation origin with the configured remote."},
		"flatpak.scope":     {Consumer: "BaseAdapter.Observe", Rationale: "The verification query is scoped to the selected Flatpak installation."},
		"snap.channel":      {Consumer: "BaseAdapter.Observe", Rationale: "The requested channel is compared with Snap's reported tracking channel."},
		"snap.track":        {Consumer: "BaseAdapter.Observe", Rationale: "The requested track is compared with Snap's reported tracking channel."},
		"snap.risk":         {Consumer: "BaseAdapter.Observe", Rationale: "The requested risk is compared with Snap's reported tracking channel."},
		"snap.branch":       {Consumer: "BaseAdapter.Observe", Rationale: "The requested branch is compared with Snap's reported tracking channel."},
	})
}
