package gitobject

import "testing"

func TestValidID(t *testing.T) {
	for _, value := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		if !ValidID(value) {
			t.Errorf("ValidID(%q) = false", value)
		}
	}
	for _, value := range []string{
		"",
		"abcdef",
		"0123456789ABCDEF0123456789ABCDEF01234567",
		"g123456789abcdef0123456789abcdef01234567",
	} {
		if ValidID(value) {
			t.Errorf("ValidID(%q) = true", value)
		}
	}
}
