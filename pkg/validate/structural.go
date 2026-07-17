package validate

import (
	"fmt"
	"strings"

	"depengine/pkg/schema"
)

// validateRequiredFields checks that method-kind-specific required fields
// are present in each method candidate's Config blob.
//
// Each method kind has its own contract:
//   - git:  url (string)
//   - http: url (string)
//   - cargo: when git sub-key present, the value must be a string URL
//   - any:  build (string), extract_to (string), checksum (string)

// commonStringKeys are config keys used by various adapters that must be
// strings. If present, their value must be of type string.
var commonStringKeys = []string{
	"pkg", "cask", "app", "source", "repo", "formula",
	"package", "bin", "command", "extra_args",
	"checksum_url", "checksum_file_format", "signature_url", "signing_key",
}

func validateRequiredFields(s *schema.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			switch mc.Kind {
			case "git":
				if v, ok := mc.Config["url"]; !ok || v == "" {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "url"),
						Message: fmt.Sprintf("git method for tool %q requires a url field", toolName),
					})
				} else if _, isStr := v.(string); !isStr {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "url"),
						Message: fmt.Sprintf("git method url must be a string, got %T", v),
					})
				}

			case "http":
				if v, ok := mc.Config["url"]; !ok || v == "" {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "url"),
						Message: fmt.Sprintf("http method for tool %q requires a url field", toolName),
					})
				} else if _, isStr := v.(string); !isStr {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "url"),
						Message: fmt.Sprintf("http method url must be a string, got %T", v),
					})
				}
			}

			// Check build field type if present (any method kind).
			if v, ok := mc.Config["build"]; ok {
				if _, isStr := v.(string); !isStr {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "build"),
						Message: fmt.Sprintf("build must be a string, got %T", v),
					})
				}
			}

			// Check extract_to type if present.
			if v, ok := mc.Config["extract_to"]; ok {
				if _, isStr := v.(string); !isStr {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "extract_to"),
						Message: fmt.Sprintf("extract_to must be a string, got %T", v),
					})
				}
			}

			// Check checksum type if present.
			if v, ok := mc.Config["checksum"]; ok {
				if _, isStr := v.(string); !isStr {
					r.Add(ValidationError{
						Code:    ErrRequiredField,
						Field:   fieldPath(toolName, i, "checksum"),
						Message: fmt.Sprintf("checksum must be a string, got %T", v),
					})
				}
			}

			// Validate checksum content for http methods.
			if mc.Kind == "http" {
				if v, ok := mc.Config["checksum"]; ok {
					if s, isStr := v.(string); isStr && s != "" {
						if strings.HasSuffix(s, ":auto") {
							r.Add(ValidationError{
								Code:    WarnAutoChecksum,
								Field:   fieldPath(toolName, i, "checksum"),
								Message: fmt.Sprintf("checksum %q uses :auto — TOFU (Trust On First Use) applies, hash is NOT verified", s),
							})
						} else {
							matched := false
							for _, algo := range []struct {
								prefix string
								length int
							}{
								{"sha256:", 64},
								{"sha512:", 128},
								{"sha1:", 40},
								{"md5:", 32},
							} {
								if strings.HasPrefix(s, algo.prefix) {
									hexPart := s[len(algo.prefix):]
									if len(hexPart) != algo.length || !isHexString(hexPart) {
										r.Add(ValidationError{
											Code:    ErrInvalidChecksum,
											Field:   fieldPath(toolName, i, "checksum"),
											Message: fmt.Sprintf("checksum %q has invalid format: expected %d hex characters after %s", s, algo.length, algo.prefix),
										})
									}
									matched = true
									break
								}
							}
							if !matched {
								r.Add(ValidationError{
									Code:    ErrInvalidValue,
									Field:   fieldPath(toolName, i, "checksum"),
									Message: fmt.Sprintf("checksum %q does not use a recognized prefix (sha256:, sha512:, sha1:, md5:)", s),
								})
							}
						}
					}
				}

				// Validate checksum_file_format if present.
				if v, ok := mc.Config["checksum_file_format"]; ok {
					if s, isStr := v.(string); isStr && s != "" {
						switch s {
						case "sha256sum", "bsd", "raw":
						default:
							r.Add(ValidationError{
								Code:    ErrInvalidValue,
								Field:   fieldPath(toolName, i, "checksum_file_format"),
								Message: fmt.Sprintf("checksum_file_format must be one of \"sha256sum\", \"bsd\", or \"raw\", got %q", s),
							})
						}
					}
				}
			}

			// If cargo has a git sub-key, it must be a string URL.
			if mc.Kind == "cargo" {
				if v, ok := mc.Config["git"]; ok {
					if _, isStr := v.(string); !isStr {
						r.Add(ValidationError{
							Code:    ErrRequiredField,
							Field:   fieldPath(toolName, i, "git"),
							Message: fmt.Sprintf("cargo git must be a string URL, got %T", v),
						})
					}
				}
			}

			// Validate common string config keys for type correctness.
			// These keys are used by various adapters and must be strings
			// when present.
			for _, key := range commonStringKeys {
				if v, ok := mc.Config[key]; ok {
					if _, isStr := v.(string); !isStr {
						r.Add(ValidationError{
							Code:    ErrRequiredField,
							Field:   fieldPath(toolName, i, key),
							Message: fmt.Sprintf("%s must be a string, got %T", key, v),
						})
					}
				}
			}
		}
	}
	return r
}

// validateWhenDirectives checks that when clauses only use known keys.
// Currently the only supported key is "distro_family".
func validateWhenDirectives(s *schema.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			if mc.When == nil {
				continue
			}

			// If When is non-nil but DistroFamily is empty, it means the
			// when clause had keys the parser didn't recognize.
			if len(mc.When.DistroFamily) == 0 {
				r.Add(ValidationError{
					Code:    WarnUnknownWhenKey,
					Field:   fieldPath(toolName, i, "when"),
					Message: fmt.Sprintf("tool %q has an empty when clause — possible unrecognized key(s)", toolName),
				})
			}
		}
	}

	return r
}

// validatePlaceholders scans every string leaf in the schema (tool names,
// method config values, postinstall, requires) and flags {name} tokens
// that are not in the known set.
func validatePlaceholders(s *schema.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		// Check requires entries.
		for _, dep := range tool.Requires {
			scanPlaceholders(dep, fieldPath(toolName, -1, "requires"), r)
		}

		// Check postinstall.
		if tool.PostInstall != "" {
			scanPlaceholders(tool.PostInstall, fieldPath(toolName, -1, "postinstall"), r)
		}

		// Check method configs.
		for i, mc := range tool.Methods {
			for key, val := range mc.Config {
				strVal, ok := val.(string)
				if !ok || strVal == "" {
					continue
				}
				scanPlaceholders(strVal, fieldPath(toolName, i, key), r)
			}
		}
	}

	return r
}

// scanPlaceholders extracts {name} tokens from s and flags unknown ones.
func scanPlaceholders(s, field string, r *Result) {
	matches := schema.PlaceholderRe.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		name := m[1] // captured group
		if !knownPlaceholderLookup[name] {
			r.Add(ValidationError{
				Code:    WarnUnknownPlaceholder,
				Field:   field,
				Message: fmt.Sprintf("unknown placeholder {%s} in %q", name, truncateStr(s, 80)),
			})
		}
	}
}

// isHexString reports whether every byte in s is a valid hexadecimal digit.
func isHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
