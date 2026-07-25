package git

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewGitAdapter())
}
