// Package validate implements structural, semantic, and environmental
// validation for depengine schema.toml files. It complements the basic
// method-kind checks in pkg/config.Validate with field-level scrutiny:
// required fields per method, placeholder validity, dependency cycles,
// dangling references, URL correctness, and environment readiness.
package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/native"
)

// ErrorCode is a stable identifier for a class of validation findings.
type ErrorCode string

const (
	// Errors (hard — schema is unusable).
	ErrRequiredField   ErrorCode = "E_REQUIRED_FIELD"
	ErrDanglingRef     ErrorCode = "E_DANGLING_REF"
	ErrCycle           ErrorCode = "E_CYCLE"
	ErrMalformedURL    ErrorCode = "E_MALFORMED_URL"
	ErrInvalidValue    ErrorCode = "E_INVALID_VALUE"
	ErrInvalidChecksum ErrorCode = "E_INVALID_CHECKSUM"
	ErrUnsafePackageName      ErrorCode = "E_UNSAFE_PACKAGE_NAME"
	// ErrDupeTool is produced by pkg/config.SchemaCodeError when a tool is
	// redeclared (simple list + inline table). It is emitted before
	// ValidateSchema runs, so it surfaces via ParseSchema error handling
	// rather than as a ValidationError.
	ErrDupeTool ErrorCode = "E_DUPE_TOOL"

	// Warnings (soft — schema may still work).
	WarnUnknownPlaceholder  ErrorCode = "W_UNKNOWN_PLACEHOLDER"
	WarnUnknownWhenKey      ErrorCode = "W_UNKNOWN_WHEN_KEY"
	WarnUnknownDistroFamily ErrorCode = "W_UNKNOWN_DISTRO_FAMILY"
	WarnAutoChecksum        ErrorCode = "W_AUTO_CHECKSUM"
	WarnEnvMissing          ErrorCode = "W_ENV_MISSING"
	WarnSignatureNoKey      ErrorCode = "W_SIGNATURE_NO_KEY"
)

// ValidationError represents a single validation finding.
type ValidationError struct {
	Code    ErrorCode `json:"code"`
	Field   string    `json:"field"`
	Message string    `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Field, e.Message)
}

// IsError returns true when the finding is a hard error.
func (e ValidationError) IsError() bool {
	return strings.HasPrefix(string(e.Code), "E_")
}

// Result holds all validation findings.
type Result struct {
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

// Add classifies a finding by its code prefix and appends to the right slice.
func (r *Result) Add(e ValidationError) {
	if e.IsError() {
		r.Errors = append(r.Errors, e)
	} else {
		r.Warnings = append(r.Warnings, e)
	}
}

// Merge absorbs findings from another result.
func (r *Result) Merge(other *Result) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

// HasErrors reports whether any hard errors were found.
func (r *Result) HasErrors() bool {
	return len(r.Errors) > 0
}

// All returns every finding (warnings first, then errors).
func (r *Result) All() []ValidationError {
	out := make([]ValidationError, 0, len(r.Warnings)+len(r.Errors))
	out = append(out, r.Warnings...)
	out = append(out, r.Errors...)
	return out
}

// knownPlaceholderLookup is built once from config.KnownPlaceholders(),
// the single source of truth for permitted {name} tokens.
var knownPlaceholderLookup = buildPlaceholderLookup()

func buildPlaceholderLookup() map[string]bool {
	names := config.KnownPlaceholders()
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// knownDistroFamilies is the set of values returned by engine.ResolveFamily
// as clans. Used to validate when.distro_family entries.
var knownDistroFamilies map[string]bool

func init() {
	clans := native.AllClans()
	knownDistroFamilies = make(map[string]bool, len(clans)+1)
	for _, c := range clans {
		knownDistroFamilies[c] = true
	}
	knownDistroFamilies["unknown"] = true
}

// fieldPath builds a dotted path string for a tool and method.
// Pass methodIdx = -1 for tool-level fields (no method index).
func fieldPath(toolName string, methodIdx int, field string) string {
	if methodIdx < 0 {
		if field == "" {
			return fmt.Sprintf("tools.%s", toolName)
		}
		return fmt.Sprintf("tools.%s.%s", toolName, field)
	}
	if field == "" {
		return fmt.Sprintf("tools.%s.methods[%d]", toolName, methodIdx)
	}
	return fmt.Sprintf("tools.%s.methods[%d].%s", toolName, methodIdx, field)
}

// truncateStr truncates a string for display in error messages.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// validatePackageName checks that a package name does not start with '-'
// (which would be interpreted as a flag by most package managers) and only
// contains characters valid across all supported package managers.
// Empty string is allowed (no package name = no substitution needed).
func validatePackageName(name string) *ValidationError {
	if name == "" {
		return nil
	}
	// Must start with alphanumeric (no leading dash = no flag injection).
	// Allowed chars: alphanumeric, dots, hyphens, underscores, plus,
	// at-signs, colons, and forward slashes. This covers:
	//   - apt/deb:       libfoo-dev, python3.11, g++, sgml-base
	//   - pacman:        cat/pkg, lib32-alsa, mingw-w64-x86_64-pkg
	//   - dnf/rpm:       python3-devel, gcc-c++
	//   - apk:           alpine-sdk, python3@edge
	//   - brew:          gcc@latest, user/repo
	//   - winget:        Microsoft.VCRedist, nodejs:20/current
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9._+@:/:-]*$`, name); !matched {
		return &ValidationError{
			Code:    ErrUnsafePackageName,
			Field:   "pkg",
			Message: fmt.Sprintf("unsafe package name %q: must start with alphanumeric character and contain only [a-zA-Z0-9._+@:/:-]", truncateStr(name, 80)),
		}
	}
	return nil
}

// validatePackageNames checks every method candidate's pkg and pkg_overrides
// fields for safe package naming.
func validatePackageNames(s *config.Schema) *Result {
	r := &Result{}
	for toolName, tool := range s.Tools {
		for i, method := range tool.Methods {
			if pkg, ok := method.Config["pkg"]; ok {
				if pkgStr, ok := pkg.(string); ok {
					if verr := validatePackageName(pkgStr); verr != nil {
						verr.Field = fieldPath(toolName, i, "pkg")
						r.Add(*verr)
					}
				}
			}
			// Also check pkg_overrides values.
			if overrides, ok := method.Config["pkg_overrides"]; ok {
				if overrideMap, ok := overrides.(map[string]any); ok {
					for manager, pkg := range overrideMap {
						if pkgStr, ok := pkg.(string); ok {
							if verr := validatePackageName(pkgStr); verr != nil {
								verr.Field = fieldPath(toolName, i, "pkg_overrides."+manager)
								r.Add(*verr)
							}
						}
					}
				}
			}
		}
	}
	return r
}

// ValidateSchema runs structural and semantic validation on a parsed schema,
// including method-kind checks when knownKinds is non-nil (callers that want
// adapter registration verification pass exec.RegisteredKinds(); tests that
// only check field-level validation pass nil).
func ValidateSchema(s *config.Schema, knownKinds []string) *Result {
	r := &Result{}

	// Method-kind validation (delegated from config.Validate).
	if knownKinds != nil {
		warnings, verr := config.Validate(s, knownKinds)
		if verr != nil {
			r.Add(ValidationError{
				Code:    ErrInvalidValue,
				Field:   "tools",
				Message: verr.Error(),
			})
		}
		for _, w := range warnings {
			r.Add(ValidationError{
				Code:    ErrInvalidValue,
				Field:   "tools",
				Message: w,
			})
		}
	}

	// Structural checks.
	r.Merge(validateRequiredFields(s))
	r.Merge(validateWhenDirectives(s))
	r.Merge(validatePlaceholders(s))
	r.Merge(validatePackageNames(s))

	// Semantic checks.
	r.Merge(validateCycles(s))
	r.Merge(validateDanglingReferences(s))
	r.Merge(validateMalformedURLs(s))
	r.Merge(validateUnknownDistroFamily(s))
	r.Merge(validateMethodOrderConflicts(s))
	r.Merge(validateSignatureSecurity(s))
	return r
}
