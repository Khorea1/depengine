package app

import (
	"github.com/Khorea1/depengine/internal/container"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	gitadapter "github.com/Khorea1/depengine/internal/git"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/localartifactadapter"
	"github.com/Khorea1/depengine/internal/msi"
)

func InitAdapters() {
	exec.Register(exec.NewNativeAdapter(""))
	exec.RegisterNativeManagerAliases()
	ecosystem.RegisterAll("paru")
	exec.Register(gitadapter.NewGitAdapter())
	exec.Register(localartifactadapter.NewAdapter())
	exec.Register(httpdownload.NewHTTPAdapter())
	exec.Register(httpdownload.NewGitHubAdapter())
	exec.Register(httpdownload.NewAppImageAdapter())
	exec.Register(httpdownload.NewAndroidAdapter())
	exec.Register(msi.NewAdapter())
	exec.Register(container.NewContainerAdapter())
	for _, adapter := range exec.WindowsAdapters() {
		exec.Register(adapter)
	}
}

func NormalizeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			out[i] = "-" + arg
		} else {
			out[i] = arg
		}
	}
	return out
}
