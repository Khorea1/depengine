package contracttest

func init() {
	RegisterCoverage(PhaseVerify, map[string]Coverage{
		"scoop.pkg":        {Consumer: "winAdapter.Observe", Rationale: "Changing the requested package selects a distinct package row in controlled Scoop output."},
		"scoop.version":    {Consumer: "winAdapter.Observe", Rationale: "Changing the requested version against a fixed installed version changes verification presence."},
		"scoop.bucket":     {Consumer: "winAdapter.Observe", Rationale: "Changing the requested bucket against a fixed installed source changes verification presence."},
		"scoop.scope":      {Consumer: "winAdapter.Observe", Rationale: "User and global scope issue distinct queries with independently controlled installed state."},
		"choco.pkg":        {Consumer: "winAdapter.Observe", Rationale: "Changing the requested package selects a distinct package row in controlled Chocolatey output."},
		"choco.version":    {Consumer: "winAdapter.Observe", Rationale: "Changing the requested version against a fixed installed version changes verification presence."},
		"bun.pkg":          {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package against a fixed global Bun list changes verification presence."},
		"bun.version":      {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed global Bun list changes verification presence."},
		"cask.pkg":         {Consumer: "BaseAdapter.Observe", Rationale: "Changing the package changes the Homebrew cask query result."},
		"gem.pkg":          {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct installed Ruby gem row."},
		"gem.version":      {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed installed gem version changes verification presence."},
		"pipx.pkg":         {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct pipx environment in controlled JSON state."},
		"pipx.version":     {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed pipx environment changes verification presence."},
		"npm.pkg":          {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct npm dependency in controlled JSON state."},
		"npm.version":      {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed npm dependency changes verification presence."},
		"apm.pkg":          {Consumer: "BaseAdapter.Observe", Rationale: "Changing the package selects a distinct entry in the controlled installed Atom package list."},
		"mas.pkg":          {Consumer: "BaseAdapter.Observe", Rationale: "Changing the package ID selects a distinct entry in the controlled Mac App Store list."},
		"uv.pkg":           {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct uv tool in controlled list output."},
		"uv.version":       {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed uv tool changes verification presence."},
		"vscode.pkg":       {Consumer: "BaseAdapter.Observe", Rationale: "Changing the extension ID changes exact-line matching against controlled VS Code output."},
		"vscodium.pkg":     {Consumer: "BaseAdapter.Observe", Rationale: "Changing the extension ID changes exact-line matching against controlled VSCodium output."},
		"composer.pkg":     {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct Composer package in controlled show output."},
		"composer.version": {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed Composer package changes verification presence."},
		"appman.pkg":       {Consumer: "BaseAdapter.Observe", Rationale: "Changing the package changes the controlled PATH lookup result."},
		"yarn.pkg":         {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct Yarn package in controlled global list output."},
		"yarn.version":     {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed Yarn package changes verification presence."},
		"pnpm.pkg":         {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested package selects a distinct pnpm dependency in controlled JSON state."},
		"pnpm.version":     {Consumer: "BaseAdapter.Observe", Rationale: "Changing the requested version against a fixed pnpm dependency changes verification presence."},
	})
}
