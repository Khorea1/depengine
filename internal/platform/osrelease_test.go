package platform

import (
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	got := parseOSRelease([]byte("# comment\r\nID=ubuntu\r\nNAME=\"Ubuntu\"\r\nPRETTY_NAME=\" Ubuntu 24.04 LTS \"\r\nID_LIKE=\"debian\"\r\nVERSION_ID=\"24.04\"\r\nIGNORED=value\r\n"))
	if got.ID != "ubuntu" || got.Name != "Ubuntu" || got.PrettyName != "Ubuntu 24.04 LTS" || got.IDLike != "debian" || got.VersionID != "24.04" {
		t.Fatalf("parseOSRelease() = %#v", got)
	}
}

func TestParseOSReleaseRejectsUnsafeOrInvalidValues(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := parseOSRelease([]byte("ID=123\nNAME=good\\nname\nPRETTY_NAME=bad\x01value\nVERSION_ID=123\nID_LIKE=" + long + "\n"))
	if got.ID != "" {
		t.Fatalf("numeric ID accepted: %q", got.ID)
	}
	if got.Name != "good name" {
		t.Fatalf("NAME = %q, want %q", got.Name, "good name")
	}
	if got.PrettyName != "" {
		t.Fatalf("control character accepted: %q", got.PrettyName)
	}
	if got.VersionID != "123" {
		t.Fatalf("VERSION_ID = %q, want numeric value", got.VersionID)
	}
	if len(got.IDLike) != 256 {
		t.Fatalf("ID_LIKE length = %d, want 256", len(got.IDLike))
	}
}
