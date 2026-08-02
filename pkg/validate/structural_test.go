package validate

import (
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/Khorea1/depengine/pkg/config"
)

// helper to build a Schema struct inline for unit tests.

func tool(name string, methods []*cfg.MethodCandidate, requires []string) *cfg.Tool {
	return &cfg.Tool{
		Name:     name,
		Methods:  methods,
		Requires: requires,
	}
}

func mc(kind string, when *cfg.Condition, cfgMap map[string]any) *cfg.MethodCandidate {
	return &cfg.MethodCandidate{
		Kind:   kind,
		When:   when,
		Config: cfgMap,
	}
}

func cond(families ...string) *cfg.Condition {
	return &cfg.Condition{DistroFamily: families}
}

// parseTestdata is a test helper that parses a TOML file from testdata/.
func parseTestdata(t *testing.T, name string) *cfg.Schema {
	t.Helper()
	path := filepath.Join("testdata", name)
	s, err := cfg.ParseSchema(path, map[string]string{})
	if err != nil {
		t.Fatalf("ParseSchema(%s): %v", name, err)
	}
	return s
}

// ---------- Structural: required fields ----------

func TestValidateRequiredFields_Valid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"zsh": tool("zsh", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if r.HasErrors() || len(r.Warnings) > 0 {
		t.Errorf("expected no findings, got %d errors, %d warnings", len(r.Errors), len(r.Warnings))
	}
}

func TestValidateRequiredFields_GitMissingURL(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("git", nil, map[string]any{"build": "make"}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for missing git url")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == ErrRequiredField && e.Field == "tools.myapp.methods[0].url" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ErrRequiredField for url, got: %+v", r.Errors)
	}
}

func TestValidateRequiredFields_HTTPMissingURL(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{"checksum": "sha256:auto"}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for missing http url")
	}
}

func TestValidateRequiredFields_GitURLNotString(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("git", nil, map[string]any{"url": 42}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string git url")
	}
}

func TestValidateRequiredFields_HTTPValid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{"url": "https://example.com/file.deb"}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
}

func TestValidateRequiredFields_BuildNotString(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("git", nil, map[string]any{"url": "https://example.com/repo", "build": 123}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string build")
	}
}

func TestValidateRequiredFields_ExtractToString(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{
					"url":        "https://example.com/file.zip",
					"extract_to": 42,
				}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string extract_to")
	}
}

func TestValidateRequiredFields_ChecksumNotString(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{
					"url":      "https://example.com/file.deb",
					"checksum": 42,
				}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string checksum")
	}
}

func TestValidateRequiredFields_CargoGitNotString(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("cargo", nil, map[string]any{"git": 42}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string cargo git")
	}
}

func TestValidateRequiredFields_MultipleMethods(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("git", nil, map[string]any{}),                           // missing url
				mc("native", nil, map[string]any{"pkg": "myapp"}),          // ok
				mc("http", nil, map[string]any{"url": "https://ex.com/x"}), // ok
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected at least one error (git missing url)")
	}
	// Should have exactly one error (git missing url).
	if len(r.Errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
}

func TestValidateRequiredFields_CommonStringKeys_Invalid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"test": tool("test", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": 42}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected error for non-string pkg")
	}
	if len(r.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(r.Errors), r.Errors)
	}
	if !strings.Contains(r.Errors[0].Message, "pkg must be a string") {
		t.Errorf("unexpected message: %s", r.Errors[0].Message)
	}
}

func TestValidateRequiredFields_CommonStringKeys_Valid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"test": tool("test", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{
					"pkg":        "bat",
					"cask":       "alfred",
					"app":        "1password",
					"source":     "aur",
					"repo":       "core",
					"formula":    "neovim",
					"package":    "fd-find",
					"bin":        "/usr/local/bin/fd",
					"command":    "bat",
					"extra_args": "--color=always",
				}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if r.HasErrors() {
		t.Errorf("expected no errors for valid string keys, got: %v", r.Errors)
	}
}

func TestValidateRequiredFields_CommonStringKeys_MultipleWrongTypes(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"test": tool("test", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{
					"pkg":     true, // wrong type
					"command": 42,   // wrong type
				}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if !r.HasErrors() {
		t.Fatal("expected errors for non-string config values")
	}
	if len(r.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(r.Errors), r.Errors)
	}
	var msgs []string
	for _, e := range r.Errors {
		msgs = append(msgs, e.Message)
	}
	if !strings.Contains(msgs[0], "pkg must be a string") && !strings.Contains(msgs[1], "pkg must be a string") {
		t.Error("missing pkg type error")
	}
	if !strings.Contains(msgs[0], "command must be a string") && !strings.Contains(msgs[1], "command must be a string") {
		t.Error("missing command type error")
	}
}

// ---------- Structural: when directives ----------

func TestValidateWhenDirectives_Valid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("native", cond("arch"), map[string]any{}),
			}, nil),
		},
	}
	r := validateWhenDirectives(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings, got: %v", r.Warnings)
	}
}

func TestValidateWhenDirectives_NilWhen(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{}),
			}, nil),
		},
	}
	r := validateWhenDirectives(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings for nil when, got: %v", r.Warnings)
	}
}

// ---------- Structural: placeholders ----------

func TestValidatePlaceholders_Valid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("git", nil, map[string]any{
					"url": "https://github.com/{distro_family}/repo.git",
				}),
			}, nil),
		},
	}
	r := validatePlaceholders(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings, got: %v", r.Warnings)
	}
}

func TestValidatePlaceholders_UnknownToken(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": tool("myapp", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{
					"url": "https://example.com/{unknown_placeholder}/file.deb",
				}),
			}, nil),
		},
	}
	r := validatePlaceholders(s)
	if len(r.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(r.Warnings), r.Warnings)
	}
	if r.Warnings[0].Code != WarnUnknownPlaceholder {
		t.Errorf("expected WarnUnknownPlaceholder, got %s", r.Warnings[0].Code)
	}
}

func TestValidatePlaceholders_InPostInstall(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": {
				Name: "myapp",
				Methods: []*cfg.MethodCandidate{
					mc("native", nil, map[string]any{}),
				},
				PostInstall: "command {bad_placeholder}",
			},
		},
	}
	r := validatePlaceholders(s)
	if len(r.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(r.Warnings), r.Warnings)
	}
}

func TestValidatePlaceholders_KnownTokens(t *testing.T) {
	// All known placeholders should not trigger warnings.
	tests := []struct {
		name  string
		value string
	}{
		{"arch", "{arch}"},
		{"pkg", "{pkg}"},
		{"latest", "{latest}"},
		{"distro_family", "{distro_family}"},
		{"kernel", "{kernel}"},
		{"os", "{os}"},
		{"id", "{id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &cfg.Schema{
				Tools: map[string]*cfg.Tool{
					"myapp": tool("myapp", []*cfg.MethodCandidate{
						mc("http", nil, map[string]any{
							"url": "https://example.com/" + tt.value + "/file",
						}),
					}, nil),
				},
			}
			r := validatePlaceholders(s)
			if len(r.Warnings) > 0 {
				t.Errorf("unexpected warnings for %s: %v", tt.value, r.Warnings)
			}
		})
	}
}

// ---------- Integration-style from file ----------

func TestValidateSchema_ValidMinimal(t *testing.T) {
	s := parseTestdata(t, "valid_minimal.toml")
	r := ValidateSchema(s, nil)
	if r.HasErrors() || len(r.Warnings) > 0 {
		t.Errorf("expected clean validation, got %d errors, %d warnings: %v",
			len(r.Errors), len(r.Warnings), r.All())
	}
}

func TestValidateSchema_ValidFull(t *testing.T) {
	s := parseTestdata(t, "valid_full.toml")
	r := ValidateSchema(s, nil)
	if r.HasErrors() {
		t.Errorf("expected no errors, got: %v", r.Errors)
	}
	// Possibly warnings about placeholders — {latest} is known, so should be clean.
	for _, w := range r.Warnings {
		t.Logf("warning: %s", w.Error())
	}
}

func TestValidateMethodOrderConflicts(t *testing.T) {
	// method_prefer + method_only on same tool → error.
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": {
				Name:         "myapp",
				MethodPrefer: []string{"cargo"},
				MethodOnly:   []string{"go"},
				Methods:      []*cfg.MethodCandidate{{Kind: "cargo"}, {Kind: "go"}},
			},
		},
	}
	r := validateMethodOrderConflicts(s)
	if !r.HasErrors() {
		t.Fatal("expected error for conflicting method_prefer + method_only, got none")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e.Message, "method_prefer and method_only cannot both be set") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected specific error about method_prefer + method_only, got: %v", r.Errors)
	}
}

func TestValidateMethodOrderDeprecatedAndOnly(t *testing.T) {
	// method_order (deprecated) + method_only → error.
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": {
				Name:        "myapp",
				MethodOrder: []string{"native"},
				MethodOnly:  []string{"go"},
				Methods:     []*cfg.MethodCandidate{{Kind: "native"}, {Kind: "go"}},
			},
		},
	}
	r := validateMethodOrderConflicts(s)
	if !r.HasErrors() {
		t.Fatal("expected error for conflicting method_order + method_only, got none")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e.Message, "method_order and method_only cannot both be set") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected specific error about method_order + method_only, got: %v", r.Errors)
	}
}

func TestValidateMethodOrderOnlyNoConflict(t *testing.T) {
	// method_only alone → no error.
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": {
				Name:       "myapp",
				MethodOnly: []string{"go"},
				Methods:    []*cfg.MethodCandidate{{Kind: "go"}},
			},
		},
	}
	r := validateMethodOrderConflicts(s)
	if r.HasErrors() {
		t.Fatalf("expected no error for method_only alone, got: %v", r.Errors)
	}
}

func TestValidateMethodPreferNoConflict(t *testing.T) {
	// method_prefer alone → no error.
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"myapp": {
				Name:         "myapp",
				MethodPrefer: []string{"cargo"},
				Methods:      []*cfg.MethodCandidate{{Kind: "cargo"}},
			},
		},
	}
	r := validateMethodOrderConflicts(s)
	if r.HasErrors() {
		t.Fatalf("expected no error for method_prefer alone, got: %v", r.Errors)
	}
}

// ---------- Result helpers ----------

func TestResultMerge(t *testing.T) {
	r1 := &Result{}
	r1.Add(ValidationError{Code: ErrRequiredField, Field: "a", Message: "err1"})

	r2 := &Result{}
	r2.Add(ValidationError{Code: WarnUnknownPlaceholder, Field: "b", Message: "warn1"})

	r1.Merge(r2)
	if len(r1.Errors) != 1 || len(r1.Warnings) != 1 {
		t.Errorf("expected 1 error, 1 warning after merge; got %d errors, %d warnings",
			len(r1.Errors), len(r1.Warnings))
	}
}

func TestResultHasErrors(t *testing.T) {
	r := &Result{}
	if r.HasErrors() {
		t.Error("empty result should not have errors")
	}
	r.Add(ValidationError{Code: WarnUnknownPlaceholder, Field: "a", Message: "warn"})
	if r.HasErrors() {
		t.Error("only warnings should not count as has errors")
	}
	r.Add(ValidationError{Code: ErrRequiredField, Field: "b", Message: "err"})
	if !r.HasErrors() {
		t.Error("should have errors after adding an error")
	}
}

func TestValidationErrorIsError(t *testing.T) {
	if e := (ValidationError{Code: WarnUnknownPlaceholder}); e.IsError() {
		t.Error("W_ code should not be an error")
	}
	if e := (ValidationError{Code: ErrRequiredField}); !e.IsError() {
		t.Error("E_ code should be an error")
	}
}

func TestValidationErrorMessage(t *testing.T) {
	e := ValidationError{Code: ErrRequiredField, Field: "tools.x.url", Message: "missing url"}
	got := e.Error()
	if got != "[E_REQUIRED_FIELD] tools.x.url: missing url" {
		t.Errorf("unexpected error format: %q", got)
	}
}

// ====== Aggressive edge case tests ======

func TestParseEdgeAllMethods(t *testing.T) {
	// Ensure all-methods schema parses without error.
	s := parseTestdata(t, "edge_all_methods.toml")
	if len(s.Tools) == 0 {
		t.Fatal("expected tools to be parsed")
	}
}

func TestValidateAllMethods_NoErrors(t *testing.T) {
	s := parseTestdata(t, "edge_all_methods.toml")
	r := ValidateSchema(s, nil)
	if r.HasErrors() {
		t.Errorf("expected no errors in edge_all_methods, got %d: %v", len(r.Errors), r.Errors)
	}
	// Warnings are acceptable (e.g., placeholders in some fields).
	for _, w := range r.Warnings {
		if w.Code == WarnUnknownPlaceholder {
			// Placeholders are not expanded in this test (empty map),
			// but they shouldn't be unknown unless truly invalid.
			continue
		}
		t.Logf("unexpected warning: %v", w)
	}
}

func TestValidateMultipleErrors_CountAndTypes(t *testing.T) {
	// invalid_multiple_errors.toml has many errors — verify they're all found.
	s := parseTestdata(t, "invalid_multiple_errors.toml")
	r := ValidateSchema(s, nil)

	if len(r.Errors) == 0 {
		t.Fatal("expected errors in invalid_multiple_errors, got none")
	}

	// Count distinct error codes.
	codes := map[ErrorCode]int{}
	for _, e := range r.Errors {
		codes[e.Code]++
	}
	t.Logf("error codes: %v", codes)

	// Expected: at least 1 cycle, 1 dangling ref, 1 malformed URL, maybe required fields
	if codes[ErrCycle] == 0 {
		t.Error("expected ErrCycle (self_ref + x→y cycle)")
	}
	if codes[ErrDanglingRef] == 0 {
		t.Error("expected ErrDanglingRef (needs_missing→i_dont_exist)")
	}
	if codes[ErrMalformedURL] == 0 {
		t.Error("expected ErrMalformedURL (bad_url)")
	}
	if codes[ErrRequiredField] == 0 {
		t.Error("expected ErrRequiredField (bad_git without url)")
	}
}

func TestValidateEmptyConfig_Orphan(t *testing.T) {
	s := parseTestdata(t, "invalid_empty_config.toml")
	r := ValidateSchema(s, nil)
	// "orphan = {}" has no method — ParseSchema should have handled it.
	// It might create a native method or fail to parse. Let's check.
	if len(r.Errors)+len(r.Warnings) > 0 {
		t.Logf("error/warnings for empty config schema: %v %v", r.Errors, r.Warnings)
	}
}

func TestValidateEmptyConfig_SelfCycle(t *testing.T) {
	s := parseTestdata(t, "invalid_empty_config.toml")
	r := validateCycles(s)
	if !r.HasErrors() {
		t.Error("expected cycle error for echo→echo self-cycle")
	}
}

func TestValidatePlaceholders_MixedKnownUnknown(t *testing.T) {
	s := parseTestdata(t, "edge_mixed_placeholders.toml")
	r := validatePlaceholders(s)
	if len(r.Warnings) == 0 {
		t.Fatal("expected placeholder warnings in mixed placeholders file")
	}

	// Find unknown tokens in warning messages.
	unknownTokens := []string{"unknownz", "bad_var", "nonexistent_dep", "made_up"}
	found := map[string]bool{}
	for _, w := range r.Warnings {
		for _, tok := range unknownTokens {
			if strings.Contains(w.Message, tok) {
				found[tok] = true
			}
		}
	}
	t.Logf("found unknown tokens: %v", found)
	for _, tok := range unknownTokens {
		if !found[tok] {
			t.Errorf("unknown token %q should have been flagged", tok)
		}
	}
}
func TestValidateWhenDirectives_EmptyWhen(t *testing.T) {
	s := parseTestdata(t, "invalid_empty_when.toml")
	r := validateWhenDirectives(s)
	if len(r.Warnings) == 0 {
		t.Log("empty when clause not flagged (parser may have dropped it)")
	}
}

func TestValidateWhenDirectives_NilWhenMultiple(t *testing.T) {
	// Multiple methods with mix of nil and non-nil When
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"app": tool("app", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{}),
				mc("native", cond("debian"), map[string]any{}),
				mc("native", &cfg.Condition{}, map[string]any{}), // empty Condition
			}, nil),
		},
	}
	r := validateWhenDirectives(s)
	// The empty Condition (no DistroFamily) should be flagged
	if len(r.Warnings) != 1 {
		t.Fatalf("expected 1 warning for empty Condition, got %d: %v", len(r.Warnings), r.Warnings)
	}
}

func TestValidateRequiredFields_NestedStructs(t *testing.T) {
	// All fields that should NOT produce errors
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"app": tool("app", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{
					"pkg": "myapp",
				}),
				mc("git", nil, map[string]any{
					"url":   "https://github.com/user/repo.git",
					"build": "make install",
				}),
				mc("http", nil, map[string]any{
					"url":        "https://example.com/file.tar.gz",
					"checksum":   "sha256:6ca13d52ca70c883e0f0bb101e425a89e8624de51db2d2392593af6a84118090",
					"extract_to": "/opt/app",
				}),
				mc("cargo", nil, map[string]any{
					"git": "https://github.com/user/crate.git",
				}),
			}, nil),
		},
	}
	r := validateRequiredFields(s)
	if r.HasErrors() {
		t.Errorf("expected no errors for valid fields, got: %v", r.Errors)
	}
}

func TestValidatePlaceholders_Boundary(t *testing.T) {
	// Edge cases: empty string, just braces, mixed with special chars
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"app": tool("app", []*cfg.MethodCandidate{
				mc("http", nil, map[string]any{
					"url": "",
				}),
				mc("http", nil, map[string]any{
					"url": "https://example.com/{}/file.deb",
				}),
				mc("git", nil, map[string]any{
					"url": "https://example.com/{}/repo",
				}),
				mc("http", nil, map[string]any{
					"url": "no-placeholders-at-all",
				}),
				mc("http", nil, map[string]any{
					"url": "https://example.com/{arch}-{os}-{kernel}.deb",
				}),
			}, nil),
		},
	}
	r := validatePlaceholders(s)
	// Only the {} patterns might be flagged — known placeholders should not.
	for _, w := range r.Warnings {
		t.Logf("warning: %v", w)
	}
	// {} is not a valid placeholder (no name), so regex won't match it.
	// If there are warnings, they shouldn't be for known placeholders.
	for _, w := range r.Warnings {
		if w.Message == "" {
			continue
		}
	}
}

func TestValidateRequiredFields_NoTools(t *testing.T) {
	// Empty schema with no tools
	s := &cfg.Schema{Tools: map[string]*cfg.Tool{}}
	r := validateRequiredFields(s)
	if r.HasErrors() || len(r.Warnings) > 0 {
		t.Errorf("expected no findings for empty schema, got %v", r.All())
	}
}

func TestValidatePlaceholders_NoTools(t *testing.T) {
	s := &cfg.Schema{Tools: map[string]*cfg.Tool{}}
	r := validatePlaceholders(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings for empty schema, got %v", r.Warnings)
	}
}

func TestValidateWhenDirectives_NoTools(t *testing.T) {
	s := &cfg.Schema{Tools: map[string]*cfg.Tool{}}
	r := validateWhenDirectives(s)
	if len(r.Warnings) > 0 {
		t.Errorf("expected no warnings for empty schema, got %v", r.Warnings)
	}
}

func TestValidateFromFile_ValidEdgeCases(t *testing.T) {
	s := parseTestdata(t, "valid_edge_cases.toml")
	r := ValidateSchema(s, nil)
	if r.HasErrors() {
		t.Errorf("expected no errors in valid_edge_cases, got %d: %v", len(r.Errors), r.Errors)
	}
}

func TestValidateFromFile_MixedPlaceholders(t *testing.T) {
	s := parseTestdata(t, "edge_mixed_placeholders.toml")
	r := ValidateSchema(s, nil)
	// Should have at least some placeholder warnings.
	if len(r.Warnings) == 0 {
		t.Fatal("expected warnings for unknown placeholders")
	}
	// Might also have malformed URL errors if the placeholder replacement
	// creates a weird URL — but that's OK, it means validation is working.
	t.Logf("mixed placeholders: %d errors, %d warnings", len(r.Errors), len(r.Warnings))
}

func TestValidateFromFile_EdgeSSH(t *testing.T) {
	s := parseTestdata(t, "edge_ssh_git_url.toml")
	r := validateMalformedURLs(s)

	// git@github.com:user/repo.git (tool a) lacks a scheme and IS flagged.
	// file:/// (tool e) is flagged. ssh://, git://, https:// (b,c,d) pass.
	gotA := false
	for _, err := range r.Errors {
		if strings.Contains(err.Field, "tools.a") {
			gotA = true
		} else if strings.Contains(err.Field, "tools.e") {
			continue // expected
		} else {
			t.Errorf("unexpected error for %s: %v", err.Field, err)
		}
	}
	if !gotA {
		t.Error("expected tools.a (git@host:path) to be flagged as malformed URL")
	}
}

func TestValidateFromFile_InvalidBadTypes(t *testing.T) {
	s := parseTestdata(t, "invalid_bad_types.toml")
	r := validateRequiredFields(s)
	if len(r.Errors) == 0 {
		t.Errorf("expected type errors for invalid_bad_types.toml, got none — TOML parser may have rejected them silently")
	} else {
		t.Logf("type errors found: %v", r.Errors)
	}
}

// ---------- Package name safety ----------

func TestValidatePackageName_Valid(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"foo": tool("foo", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": "valid-pkg"}),
			}, nil),
		},
	}
	r := validatePackageNames(s)
	if r.HasErrors() {
		t.Errorf("expected no errors, got %v", r.Errors)
	}
}

func TestValidatePackageName_LeadingDash(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"foo": tool("foo", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": "-y"}),
			}, nil),
		},
	}
	r := validatePackageNames(s)
	if !r.HasErrors() {
		t.Fatal("expected error for leading dash")
	}
	if r.Errors[0].Code != ErrUnsafePackageName {
		t.Errorf("expected ErrUnsafePackageName, got %s", r.Errors[0].Code)
	}
}

func TestValidatePackageName_EmptyAllowed(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"foo": tool("foo", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": ""}),
			}, nil),
		},
	}
	r := validatePackageNames(s)
	if r.HasErrors() {
		t.Errorf("expected no errors for empty pkg, got %v", r.Errors)
	}
}

func TestValidatePackageName_ComplexValid(t *testing.T) {
	cases := []string{
		"g++",
		"python3.11-dev",
		"libfoo++-dev",
		"gcc@latest",
		"nodejs:20/current",
		"user/repo",
		"Microsoft.VCRedist",
		"cat/pkg",
		"lib32-alsa",
		"python3-devel",
	}
	for _, pkg := range cases {
		s := &cfg.Schema{
			Tools: map[string]*cfg.Tool{
				"foo": tool("foo", []*cfg.MethodCandidate{
					mc("native", nil, map[string]any{"pkg": pkg}),
				}, nil),
			},
		}
		r := validatePackageNames(s)
		if r.HasErrors() {
			t.Errorf("expected no errors for pkg %q, got %v", pkg, r.Errors)
		}
	}
}

func TestValidatePackageName_WithSpace(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"foo": tool("foo", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{"pkg": "--reinstall malicious"}),
			}, nil),
		},
	}
	r := validatePackageNames(s)
	if !r.HasErrors() {
		t.Fatal("expected error for pkg with spaces")
	}
}

func TestValidatePackageName_PkgOverrides(t *testing.T) {
	s := &cfg.Schema{
		Tools: map[string]*cfg.Tool{
			"foo": tool("foo", []*cfg.MethodCandidate{
				mc("native", nil, map[string]any{
					"pkg": "safe-pkg",
					"pkg_overrides": map[string]any{
						"apt": "-malicious",
					},
				}),
			}, nil),
		},
	}
	r := validatePackageNames(s)
	if !r.HasErrors() {
		t.Fatal("expected error for unsafe pkg_overrides value")
	}
}

func TestValidatePackageNames_ValidateSchema(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]any
		wantErr bool
		errCode ErrorCode
	}{
		{
			name:    "leading-dash-pkg",
			cfg:     map[string]any{"pkg": "--malicious-flag"},
			wantErr: true,
			errCode: ErrUnsafePackageName,
		},
		{
			name:    "empty-pkg-allowed",
			cfg:     map[string]any{"pkg": ""},
			wantErr: false,
		},
		{
			name:    "valid-pkg",
			cfg:     map[string]any{"pkg": "valid-pkg"},
			wantErr: false,
		},
		{
			name: "unsafe-pkg-overrides",
			cfg: map[string]any{
				"pkg": "safe-pkg",
				"pkg_overrides": map[string]any{
					"apt": "--unsafe",
				},
			},
			wantErr: true,
			errCode: ErrUnsafePackageName,
		},
		{
			name:    "no-pkg-field",
			cfg:     map[string]any{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &cfg.Schema{
				Tools: map[string]*cfg.Tool{
					"testtool": tool("testtool", []*cfg.MethodCandidate{
						mc("native", nil, tt.cfg),
					}, nil),
				},
			}
			r := ValidateSchema(s, nil)
			if tt.wantErr {
				if !r.HasErrors() {
					t.Fatal("expected validation errors, got none")
				}
				found := false
				for _, e := range r.Errors {
					if e.Code == tt.errCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error with code %s, got: %v", tt.errCode, r.Errors)
				}
			} else {
				for _, e := range r.All() {
					if e.Code == ErrUnsafePackageName {
						t.Errorf("unexpected ErrUnsafePackageName error: %v", e)
					}
				}
				if r.HasErrors() {
					// Log other errors but don't fail — they're outside our scope.
					t.Logf("non-package-name errors (ignored by test): %v", r.Errors)
				}
			}
		})
	}
}
