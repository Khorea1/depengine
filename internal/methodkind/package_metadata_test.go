package methodkind

import "testing"

func TestPackageMetadataFor(t *testing.T) {
	cases := []struct {
		kind          string
		componentType string
		purlType      string
	}{
		{"cargo", PackageComponentLibrary, "cargo"},
		{"go", PackageComponentLibrary, "golang"},
		{"pipx", PackageComponentLibrary, "pypi"},
		{"pnpm", PackageComponentLibrary, "npm"},
		{"container", PackageComponentApplication, "oci"},
		{"native", PackageComponentApplication, "native"},
		{"yay", PackageComponentApplication, "yay"},
		{"custom", PackageComponentApplication, "custom"},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := PackageMetadataFor(tc.kind)
			if got.ComponentType != tc.componentType {
				t.Errorf("ComponentType = %q, want %q", got.ComponentType, tc.componentType)
			}
			if got.PURLType != tc.purlType {
				t.Errorf("PURLType = %q, want %q", got.PURLType, tc.purlType)
			}
		})
	}
}
