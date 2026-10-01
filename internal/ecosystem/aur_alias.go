package ecosystem

import (
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
)

// AURByNameAdapter registers an AUR helper under its binary name. This lets
// schema entries resolve to the AUR adapter directly, bypassing the need for a
// `defaults.aur_helper` indirection.
type AURByNameAdapter struct {
	*AURAdapter
	name string
}

func (a *AURByNameAdapter) Kind() string { return a.name }

// RegisterAURAliases registers the AUR method aliases as named adapter kinds,
// each delegating to AURAdapter with the corresponding helper binary.
func RegisterAURAliases() {
	contract, ok := methodkind.Lookup("aur")
	if !ok || contract == nil {
		panic("ecosystem: missing aur method contract")
	}

	for _, name := range contract.Aliases {
		exec.Register(&AURByNameAdapter{
			AURAdapter: NewAURAdapter(name),
			name:       name,
		})
	}
}

var _ exec.AdapterV2 = (*AURByNameAdapter)(nil)
