package container

import (
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewContainerAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("container") != nil {
		t.Fatal("importing pkg/container registered an adapter")
	}
}
