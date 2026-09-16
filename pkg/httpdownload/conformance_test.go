package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewHTTPAdapter())
}

func TestImportDoesNotRegisterAdapters(t *testing.T) {
	for _, kind := range []string{"http", "github", "appimage", "android"} {
		if exec.Lookup(kind) != nil {
			t.Errorf("importing pkg/httpdownload registered %q", kind)
		}
	}
}
