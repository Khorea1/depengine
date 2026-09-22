package git

import (
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewGitAdapter())
}

func TestImportDoesNotRegisterAdapter(t *testing.T) {
	if exec.Lookup("git") != nil {
		t.Fatal("importing internal/git registered an adapter")
	}
}
