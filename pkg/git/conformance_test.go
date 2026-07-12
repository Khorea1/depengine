package git

import (
	"testing"

	"depengine/pkg/exectest"
)

func TestConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewGitAdapter())
}
