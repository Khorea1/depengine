package platform

import "testing"

func TestResolveFamilyTable(t *testing.T) {
	cases := []struct {
		name       string
		distroID   string
		distroLike string
		isAndroid  bool
		want       string
	}{
		{"ubuntu direto", "ubuntu", "debian", false, "debian"},
		{"arch direto", "arch", "", false, "arch"},
		{"manjaro direto", "manjaro", "arch", false, "arch"},
		{"cachyos direto", "cachyos", "", false, "arch"},
		{"fedora direto", "fedora", "", false, "fedora"},
		{"opensuse via id_like", "opensuse-tumbleweed", "suse", false, "suse"},
		{"alpine direto", "alpine", "", false, "alpine"},
		{"macos direto", "macos", "", false, "macos"},
		{"termux direto", "termux", "", true, "termux"},
		{"distro desconhecida com id_like arch", "cyberos", "arch", false, "arch"},
		{"distro desconhecida com id_like rhel fedora", "rockylinux9-nightly", "rhel fedora", false, "fedora"},
		{"totalmente desconhecida", "plan9-frontend", "", false, "unknown"},
		{"android puro sem id_like", "android", "", true, "android"},
		{"void direto", "void", "", false, "void"},
		{"linuxmint direto", "linuxmint", "", false, "mint"},
		{"openwrt direto", "openwrt", "", false, "opkg"},
		{"lede direto", "lede", "", false, "opkg"},
		{"windows stub clan", "windows", "", false, "windows"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Facts{DistroID: tc.distroID, DistroIDLike: tc.distroLike, IsAndroid: tc.isAndroid}
			if got := ResolveFamily(f); got != tc.want {
				t.Fatalf("ResolveFamily = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchesDistroFamily(t *testing.T) {
	if !MatchesDistroFamily("arch", []string{"debian", "arch"}) {
		t.Fatal("arch in [debian,arch] should match")
	}
	if MatchesDistroFamily("arch", []string{"debian", "fedora"}) {
		t.Fatal("arch in [debian,fedora] should not match")
	}
	if !MatchesDistroFamily("Arch", []string{"ARCH"}) { // case-insensitive
		t.Fatal("MatchesDistroFamily should be case-insensitive")
	}
	if !MatchesDistroFamily("unknown", []string{"unknown"}) {
		t.Fatal("unknown clan should match itself")
	}
}

// TestResolveFamilyNilFacts ensures ResolveFamily returns "unknown" on nil *Facts.
func TestResolveFamilyNilFacts(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveFamily panicked on nil *Facts: %v", r)
		}
	}()

	_ = ResolveFamily(nil)
}

// TestMatchesDistroFamilyNilSlice handles edge case of nil allowed list.
func TestMatchesDistroFamilyNilSlice(t *testing.T) {
	if MatchesDistroFamily("arch", nil) {
		t.Fatal("MatchesDistroFamily should return false for nil allowed list")
	}
}

