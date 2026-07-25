// Package sbom provides SBOM (Software Bill of Materials) export for depengine.
// It supports CycloneDX 1.5 and SPDX 2.3 formats, mapping installed tools
// from depengine's state file into standardized BOM components.
package sbom

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/Khorea1/depengine/pkg/state"
)

// ─── CycloneDX 1.5 ────────────────────────────────────────────────────────────

// CycloneDXBOM represents a minimal CycloneDX 1.5 bill-of-materials document.
type CycloneDXBOM struct {
	BOMFormat   string               `json:"bomFormat"`
	SpecVersion string               `json:"specVersion"`
	Version     int                  `json:"version"`
	Metadata    CycloneDXMetadata    `json:"metadata"`
	Components  []CycloneDXComponent `json:"components"`
}

// CycloneDXMetadata holds the BOM-level metadata section.
type CycloneDXMetadata struct {
	Timestamp string          `json:"timestamp"`
	Tools     []CycloneDXTool `json:"tools"`
}

// CycloneDXTool describes the tool that generated the BOM.
type CycloneDXTool struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// CycloneDXComponent describes a single installed tool as a BOM component.
type CycloneDXComponent struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	PURL        string `json:"purl,omitempty"`
	Description string `json:"description,omitempty"`
	BOMRef      string `json:"bom-ref,omitempty"`
}

// ExportCycloneDX produces a CycloneDX 1.5 JSON document from depengine state.
// Each installed tool becomes a component with its inferred package type and
// best-effort version extraction.
func ExportCycloneDX(s *state.State) ([]byte, error) {
	bom := CycloneDXBOM{
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: CycloneDXMetadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Tools: []CycloneDXTool{
				{Name: "depengine"},
			},
		},
		Components: make([]CycloneDXComponent, 0, len(s.Tools)),
	}

	// Sort tool names for deterministic output.
	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ts := s.Tools[name]

		// Infer component type from method.
		compType := componentType(ts.Method)

		// Extract version from config if available, fallback to "0.0.0".
		version := extractVersion(ts.Config)

		// Build purl: pkg:{type}/{name}@{version}
		purl := fmt.Sprintf("pkg:%s/%s@%s", purlType(ts.Method), name, version)

		comp := CycloneDXComponent{
			Type:    compType,
			Name:    name,
			Version: version,
			PURL:    purl,
			BOMRef:  name,
		}
		bom.Components = append(bom.Components, comp)
	}

	return json.MarshalIndent(bom, "", "  ")
}

// componentType maps depengine methods to CycloneDX component types.
// Most tool installations are "application" (curated software).
// Language-specific managers produce "library".
func componentType(method string) string {
	switch method {
	case "cargo", "go", "pip", "pipx", "uv", "npm", "pnpm", "bun",
		"gem", "yarn", "yarn-berry", "composer", "conda":
		return "library"
	default:
		return "application"
	}
}

// purlType maps depengine methods to purl package types.
// See https://github.com/package-url/purl-spec for the full type registry.
func purlType(method string) string {
	switch method {
	case "cargo":
		return "cargo"
	case "go":
		return "golang"
	case "pip", "pipx", "uv":
		return "pypi"
	case "npm", "pnpm", "bun":
		return "npm"
	case "gem":
		return "gem"
	case "conda":
		return "conda"
	case "flatpak":
		return "flatpak"
	case "snap":
		return "snap"
	case "mas":
		return "mas"
	default:
		// Generic fallback — use the method name as a purl type.
		return method
	}
}

// extractVersion tries to find a version string in the tool's config map.
// Falls back to "0.0.0" if not found.
// This is best-effort — depengine doesn't consistently track versions yet.
func extractVersion(config map[string]any) string {
	if config == nil {
		return "0.0.0"
	}
	// Common config keys that might hold version info.
	for _, key := range []string{"version", "ver", "tag", "ref"} {
		if v, ok := config[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "0.0.0"
}

// ─── SPDX 2.3 ─────────────────────────────────────────────────────────────────

// SPDXDocument represents a minimal SPDX 2.3 document.
type SPDXDocument struct {
	SPDXVersion  string        `json:"spdxVersion"`
	DataLicense  string        `json:"dataLicense"`
	SPDXID       string        `json:"SPDXID"`
	Name         string        `json:"name"`
	CreationInfo SPDXCreation  `json:"creationInfo"`
	Packages     []SPDXPackage `json:"packages"`
}

// SPDXCreation holds document creation metadata.
type SPDXCreation struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

// SPDXPackage describes a single installed tool as an SPDX package.
type SPDXPackage struct {
	Name           string `json:"name"`
	VersionInfo    string `json:"versionInfo"`
	SPDXID         string `json:"SPDXID"`
	Supplier       string `json:"supplier"`
	PrimaryPurpose string `json:"primaryPurpose,omitempty"`
}

// ExportSPDX produces an SPDX 2.3 JSON document from depengine state.
// Each installed tool becomes a package with a safe SPDX identifier.
func ExportSPDX(s *state.State) ([]byte, error) {
	doc := SPDXDocument{
		SPDXVersion: "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        "depengine-installed-tools",
		CreationInfo: SPDXCreation{
			Created:  time.Now().UTC().Format(time.RFC3339),
			Creators: []string{"Tool: depengine"},
		},
		Packages: make([]SPDXPackage, 0, len(s.Tools)),
	}

	names := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		ts := s.Tools[name]
		version := extractVersion(ts.Config)

		pkg := SPDXPackage{
			Name:        name,
			VersionInfo: version,
			SPDXID:      fmt.Sprintf("SPDXRef-%s", safeSPDXID(name)),
			Supplier:    "NOASSERTION",
		}
		doc.Packages = append(doc.Packages, pkg)
	}

	return json.MarshalIndent(doc, "", "  ")
}

// safeSPDXID converts a tool name to a valid SPDX identifier by replacing
// any non-alphanumeric character (except hyphen) with a hyphen.
func safeSPDXID(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}
