package validate

import (
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
)

// ---------- Dangling references ----------

func TestValidateDanglingReferences_Valid(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, nil),
			"b": tool("b", nil, []string{"a"}),
		},
	}
	r := validateDanglingReferences(s)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
}

func TestValidateDanglingReferences_MissingDep(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"nonexistent"}),
		},
	}
	r := validateDanglingReferences(s)
	if !r.HasErrors() {
		t.Fatal("expected error for dangling reference")
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	if r.Errors[0].Code != ErrDanglingRef {
		t.Errorf("expected ErrDanglingRef, got %s", r.Errors[0].Code)
	}
}

func TestValidateDanglingReferences_MultipleDeps(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"b", "c"}), // both missing
			"b": tool("b", nil, nil),                // defined
		},
	}
	r := validateDanglingReferences(s)
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error (only c missing), got %d: %v", len(r.Errors), r.Errors)
	}
}

// ---------- Cycles ----------

func TestValidateCycles_NoCycle(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"b"}),
			"b": tool("b", nil, []string{"c"}),
			"c": tool("c", nil, nil),
		},
	}
	r := validateCycles(s)
	if r.HasErrors() {
		t.Errorf("expected no cycle errors, got: %v", r.Errors)
	}
}

func TestValidateCycles_DirectCycle(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"b"}),
			"b": tool("b", nil, []string{"a"}),
		},
	}
	r := validateCycles(s)
	if !r.HasErrors() {
		t.Fatal("expected cycle error")
	}
	if r.Errors[0].Code != ErrCycle {
		t.Errorf("expected ErrCycle, got %s", r.Errors[0].Code)
	}
}

func TestValidateCycles_SelfCycle(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"a"}),
		},
	}
	r := validateCycles(s)
	if !r.HasErrors() {
		t.Fatal("expected cycle error for self-dependency")
	}
}

func TestValidateCycles_IndirectCycle(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, []string{"b"}),
			"b": tool("b", nil, []string{"c"}),
			"c": tool("c", nil, []string{"a"}),
		},
	}
	r := validateCycles(s)
	if !r.HasErrors() {
		t.Fatal("expected cycle error for indirect cycle")
	}
}

func TestValidateCycles_NoDeps(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"a": tool("a", nil, nil),
			"b": tool("b", nil, nil),
		},
	}
	r := validateCycles(s)
	if r.HasErrors() {
		t.Errorf("expected no errors for tools without deps, got: %v", r.Errors)
	}
}

// ---------- Malformed URLs ----------

func TestValidateMalformedURLs_ValidHTTP(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("http", nil, map[string]any{"url": "https://example.com/file.deb"}),
			}, nil),
		},
	}
	r := validateMalformedURLs(s)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
}

func TestValidateMalformedURLs_ValidGit(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("git", nil, map[string]any{"url": "https://github.com/user/repo.git"}),
			}, nil),
		},
	}
	r := validateMalformedURLs(s)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
}

func TestValidateMalformedURLs_Invalid(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("http", nil, map[string]any{"url": "not-a-valid-url"}),
			}, nil),
		},
	}
	r := validateMalformedURLs(s)
	if !r.HasErrors() {
		t.Fatal("expected error for malformed URL")
	}
}

func TestValidateMalformedURLs_WithPlaceholder(t *testing.T) {
	// URL with {latest} placeholder should be valid after replacement.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("http", nil, map[string]any{
					"url": "https://github.com/user/repo/releases/download/{latest}/file.deb",
				}),
			}, nil),
		},
	}
	r := validateMalformedURLs(s)
	if r.HasErrors() {
		t.Errorf("expected no errors (placeholder replaced), got: %v", r.Errors)
	}
}

func TestValidateMalformedURLs_NonHTTPGitKind(t *testing.T) {
	// native and cargo don't need url validation.
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": "app"}),
				mc("cargo", nil, map[string]any{}),
			}, nil),
		},
	}
	r := validateMalformedURLs(s)
	if r.HasErrors() {
		t.Errorf("expected no errors for non-url methods, got: %v", r.Errors)
	}
}

// ---------- Unknown distro family ----------

func TestValidateUnknownDistroFamily_Valid(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("native", cond("debian", "arch"), map[string]any{}),
			}, nil),
		},
	}
	r := validateUnknownDistroFamily(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings, got: %v", r.Warnings)
	}
}

func TestValidateUnknownDistroFamily_Unknown(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("native", cond("nonexistent_os"), map[string]any{}),
			}, nil),
		},
	}
	r := validateUnknownDistroFamily(s)
	if len(r.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(r.Warnings), r.Warnings)
	}
	if r.Warnings[0].Code != WarnUnknownDistroFamily {
		t.Errorf("expected WarnUnknownDistroFamily, got %s", r.Warnings[0].Code)
	}
}

func TestValidateUnknownDistroFamily_NilWhen(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("native", nil, map[string]any{}),
			}, nil),
		},
	}
	r := validateUnknownDistroFamily(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings for nil when, got: %v", r.Warnings)
	}
}

func TestValidateUnknownDistroFamily_Multiple(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("native", cond("debian", "madeup", "arch", "fake"), map[string]any{}),
			}, nil),
		},
	}
	r := validateUnknownDistroFamily(s)
	if len(r.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(r.Warnings), r.Warnings)
	}
}

// ---------- Integration from TOML files ----------

func TestValidateSchemaFromFile_InvalidDanglingRef(t *testing.T) {
	s := parseTestdata(t, "invalid_dangling_ref.toml")
	r := validateDanglingReferences(s)
	if !r.HasErrors() {
		t.Fatal("expected dangling reference error")
	}
}

func TestValidateSchemaFromFile_InvalidCycle(t *testing.T) {
	s := parseTestdata(t, "invalid_cycle.toml")
	r := validateCycles(s)
	if !r.HasErrors() {
		t.Fatal("expected cycle error")
	}
}

func TestValidateSchemaFromFile_InvalidURL(t *testing.T) {
	s := parseTestdata(t, "invalid_malformed_url.toml")
	r := validateMalformedURLs(s)
	if !r.HasErrors() {
		t.Fatal("expected malformed URL error")
	}
}

func TestValidateSchemaFromFile_InvalidPlaceholder(t *testing.T) {
	s := parseTestdata(t, "invalid_unknown_placeholder.toml")
	r := validatePlaceholders(s)
	if len(r.Warnings) == 0 {
		t.Fatal("expected placeholder warning")
	}
}

// ---------- Signature security ----------

func TestValidateSignatureSecurity_NoKey(t *testing.T) {
	s := parseTestdata(t, "warn_signature_no_key.toml")
	r := validateSignatureSecurity(s)
	if len(r.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(r.Warnings), r.Warnings)
	}
	if r.Warnings[0].Code != WarnSignatureNoKey {
		t.Errorf("expected WarnSignatureNoKey, got %s", r.Warnings[0].Code)
	}
}

func TestValidateSignatureSecurity_WithKey(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("http", nil, map[string]any{
					"url":           "https://example.com/pkg.deb",
					"signature_url": "https://example.com/pkg.deb.sig",
					"signing_key":   "ABCDEF1234567890ABCDEF1234567890ABCDEF12",
				}),
			}, nil),
		},
	}
	r := validateSignatureSecurity(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings when signing_key is present, got: %v", r.Warnings)
	}
}

func TestValidateSignatureSecurity_NoSigURL(t *testing.T) {
	s := &config.Schema{
		Tools: map[string]*config.Tool{
			"app": tool("app", []*config.MethodCandidate{
				mc("http", nil, map[string]any{
					"url": "https://example.com/pkg.deb",
				}),
			}, nil),
		},
	}
	r := validateSignatureSecurity(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings when signature_url is absent, got: %v", r.Warnings)
	}
}

func TestValidateArtifactContractsRejectUnsupportedSchemes(t *testing.T) {
	for _, kind := range []string{"http", "appimage", "android", "msi"} {
		t.Run(kind, func(t *testing.T) {
			configMap := map[string]any{"url": "ftp://example.com/tool.bin"}
			if kind == "msi" {
				configMap["url"] = "ftp://example.com/tool.msi"
				configMap["product_name"] = "Tool"
			}
			s := &config.Schema{Tools: map[string]*config.Tool{
				"app": tool("app", []*config.MethodCandidate{mc(kind, nil, configMap)}, nil),
			}}
			r := validateMalformedURLs(s)
			if !r.HasErrors() {
				t.Fatalf("expected %s to reject ftp artifact URL", kind)
			}
			if r.Errors[0].Code != ErrMalformedURL {
				t.Fatalf("code = %s, want %s", r.Errors[0].Code, ErrMalformedURL)
			}
		})
	}
}

func TestValidateArtifactContractsRejectHTTPPlatformInstallers(t *testing.T) {
	for _, ext := range []string{".msi", ".exe", ".pkg", ".dmg", ".msix", ".appx"} {
		t.Run(ext, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{
				"app": tool("app", []*config.MethodCandidate{mc("http", nil, map[string]any{"url": "https://example.com/tool" + ext})}, nil),
			}}
			r := validateMalformedURLs(s)
			if !r.HasErrors() {
				t.Fatalf("expected http %s artifact to be rejected", ext)
			}
			if r.Errors[0].Code != ErrInvalidValue {
				t.Fatalf("code = %s, want %s", r.Errors[0].Code, ErrInvalidValue)
			}
		})
	}
}

func TestValidateArtifactContractsRejectGitHubInstallerAsset(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"app": tool("app", []*config.MethodCandidate{mc("github", nil, map[string]any{
			"repo": "owner/repo", "asset": "tool-{version}.dmg",
		})}, nil),
	}}
	r := validateMalformedURLs(s)
	if !r.HasErrors() {
		t.Fatal("expected github .dmg asset to be rejected")
	}
}

func TestValidateArtifactContractsMSIRequiresMSIArtifact(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"app": tool("app", []*config.MethodCandidate{mc("msi", nil, map[string]any{
			"url": "https://example.com/tool.exe", "product_name": "Tool",
		})}, nil),
	}}
	r := validateMalformedURLs(s)
	if !r.HasErrors() {
		t.Fatal("expected msi method to reject non-msi artifact")
	}
}

func TestValidateArtifactContractsValidateAuxiliaryURLs(t *testing.T) {
	s := &config.Schema{Tools: map[string]*config.Tool{
		"app": tool("app", []*config.MethodCandidate{mc("http", nil, map[string]any{
			"url": "https://example.com/tool.tar.gz", "checksum_url": "file:///tmp/checksums.txt",
		})}, nil),
	}}
	r := validateMalformedURLs(s)
	if !r.HasErrors() {
		t.Fatal("expected non-http checksum_url to be rejected")
	}
	if got := r.Errors[0].Field; got != "tools.app.methods[0].checksum_url" {
		t.Fatalf("field = %q, want checksum_url", got)
	}
}
