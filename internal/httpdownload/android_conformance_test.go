package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/internal/exectest"
)

func TestAndroidConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewAndroidAdapter())
}
