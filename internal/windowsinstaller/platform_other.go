//go:build !windows

package windowsinstaller

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/run"
)

type unavailableCatalog struct{}

func newCatalog() packageCatalog { return unavailableCatalog{} }

func (unavailableCatalog) Find(context.Context, run.Runner, string, string, string) (string, bool, error) {
	return "", false, fmt.Errorf("windows package APIs are unavailable")
}

func installPackage(context.Context, run.Runner, string, string) error {
	return fmt.Errorf("windows package APIs are unavailable")
}

func removePackage(context.Context, run.Runner, string) error {
	return fmt.Errorf("windows package APIs are unavailable")
}
