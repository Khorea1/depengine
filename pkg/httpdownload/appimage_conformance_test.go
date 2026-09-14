package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/exectest"
)

func TestAppImageConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewAppImageAdapter())
}
