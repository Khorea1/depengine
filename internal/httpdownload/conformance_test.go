package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewHTTPAdapter())
}

func TestImportDoesNotRegisterAdapters(t *testing.T) {
	for _, kind := range []string{"http", "github", "appimage", "android"} {
		if exec.Lookup(kind) != nil {
			t.Errorf("importing internal/httpdownload registered %q", kind)
		}
	}
}
