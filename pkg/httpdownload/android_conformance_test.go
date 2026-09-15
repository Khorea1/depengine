package httpdownload

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/exectest"
)

func TestAndroidConformance(t *testing.T) {
	exectest.TestAdapterConformance(t, NewAndroidAdapter())
}
