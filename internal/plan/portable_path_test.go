package plan_test

import (
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestValidatePortablePathComponent(t *testing.T) {
	for _, valid := range []string{"tool", "tool-name", "工具", "name with space"} {
		if err := plan.ValidatePortablePathComponent(valid); err != nil {
			t.Errorf("ValidatePortablePathComponent(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{
		"", ".", "..", "tool:stream", "a?b", "name.", "name ",
		"CON", "com1.txt", "LPT9", "nul.log", "bad\x00name",
	} {
		if err := plan.ValidatePortablePathComponent(invalid); err == nil {
			t.Errorf("ValidatePortablePathComponent(%q) = nil, want error", invalid)
		}
	}
}
