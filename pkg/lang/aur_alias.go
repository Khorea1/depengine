package lang

import (
	"depengine/pkg/exec"
)

// AURByNameAdapter registers an AUR helper by its binary name (e.g. "paru",
// "yay"). This lets schema entries like `paru = "pkg"` or `yay = "pkg"`
// resolve to the AUR adapter directly, bypassing the need for a
// `defaults.aur_helper` indirection.
type AURByNameAdapter struct {
	*AURAdapter
	name string
}

func (a *AURByNameAdapter) Kind() string { return a.name }

// RegisterAURAliases registers "paru" and "yay" as named adapter kinds,
// each delegating to AURAdapter with the corresponding helper binary.
func RegisterAURAliases() {
	for _, name := range []string{"paru", "yay"} {
		exec.Register(&AURByNameAdapter{
			AURAdapter: NewAURAdapter(name),
			name:       name,
		})
	}
}

var _ exec.Adapter = (*AURByNameAdapter)(nil)
