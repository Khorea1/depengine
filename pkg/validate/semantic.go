package validate

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/artifact"
	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/containerref"
	"github.com/Khorea1/depengine/pkg/graph"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

// validateCycles detects dependency cycles using graph.Sort.
func validateCycles(s *config.Schema) *Result {
	r := &Result{}

	_, err := graph.Sort(toolsWithConditionalDependencies(s.Tools))
	if err != nil {
		var cycleErr *graph.CycleError
		if errors.As(err, &cycleErr) {
			r.Add(ValidationError{
				Code:    ErrCycle,
				Field:   "tools",
				Message: fmt.Sprintf("dependency cycle detected: %s", strings.Join(cycleErr.Cycle, " → ")),
			})
		} else {
			r.Add(ValidationError{
				Code:    ErrCycle,
				Field:   "tools",
				Message: err.Error(),
			})
		}
	}

	return r
}

// validateDanglingReferences checks that tools listed in requires actually
// exist in the schema.
func validateDanglingReferences(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		deps := append([]string(nil), tool.Requires...)
		for _, method := range tool.Methods {
			deps = append(deps, method.Requires...)
		}
		for _, dep := range deps {
			if _, ok := s.Tools[dep]; !ok {
				r.Add(ValidationError{
					Code:    ErrDanglingRef,
					Field:   fieldPath(toolName, -1, "requires"),
					Message: fmt.Sprintf("tool %q requires %q, but %q is not defined in [tools]", toolName, dep, dep),
				})
			}
		}
	}

	return r
}

func toolsWithConditionalDependencies(tools map[string]*config.Tool) map[string]*config.Tool {
	out := make(map[string]*config.Tool, len(tools))
	for name, tool := range tools {
		clone := *tool
		clone.Requires = append([]string(nil), tool.Requires...)
		seen := make(map[string]bool, len(clone.Requires))
		for _, dep := range clone.Requires {
			seen[dep] = true
		}
		for _, method := range tool.Methods {
			for _, dep := range method.Requires {
				if !seen[dep] {
					clone.Requires = append(clone.Requires, dep)
					seen[dep] = true
				}
			}
		}
		out[name] = &clone
	}
	return out
}

// validateMalformedURLs applies the artifact contract for every download-backed
// method, then preserves git's separate repository-URL validation. Keeping the
// download rules on methodkind.Contract prevents validate and runtime adapters
// from drifting on supported schemes and installer formats.
func validateMalformedURLs(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			validateSourceLikeURLs(toolName, i, mc, r)
			contract, ok := methodkind.Lookup(mc.Kind)
			if ok && contract.Artifact != nil {
				validateArtifactContract(toolName, i, mc, contract.Artifact, r)
				continue
			}
			if mc.Kind != "git" {
				continue
			}
			urlStr, _ := mc.Config["url"].(string)
			if urlStr == "" {
				continue
			}
			checkURL := config.PlaceholderRe.ReplaceAllString(urlStr, "_")
			parsed, err := url.Parse(checkURL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				r.Add(ValidationError{Code: ErrMalformedURL, Field: fieldPath(toolName, i, "url"), Message: fmt.Sprintf("malformed URL %q", urlStr)})
				continue
			}
			if (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) && parsed.User != nil {
				r.Add(ValidationError{Code: ErrMalformedURL, Field: fieldPath(toolName, i, "url"), Message: "git: embedded URL credentials are not allowed; use an external credential helper"})
			}
		}
	}

	return r
}

func validateSourceLikeURLs(toolName string, methodIdx int, mc *config.MethodCandidate, r *Result) {
	for _, field := range []string{"source", "registry", "remote", "index", "index_url"} {
		raw, _ := mc.Config[field].(string)
		if raw == "" || !strings.Contains(raw, "://") {
			continue
		}
		checkURL := config.PlaceholderRe.ReplaceAllString(raw, "_")
		if err := artifact.ValidateURL(checkURL, []string{"http", "https"}); err != nil {
			r.Add(ValidationError{Code: ErrMalformedURL, Field: fieldPath(toolName, methodIdx, field), Message: fmt.Sprintf("%s: %v", mc.Kind, err)})
		}
	}
}

func validateArtifactContract(toolName string, methodIdx int, mc *config.MethodCandidate, contract *artifact.Contract, r *Result) {
	for _, field := range contract.URLFields {
		raw, _ := mc.Config[field].(string)
		if raw == "" {
			continue
		}
		checkURL := config.PlaceholderRe.ReplaceAllString(raw, "_")
		if err := artifact.ValidateURL(checkURL, contract.AllowedSchemes); err != nil {
			r.Add(ValidationError{Code: ErrMalformedURL, Field: fieldPath(toolName, methodIdx, field), Message: fmt.Sprintf("%s: %v", mc.Kind, err)})
		}
	}

	// signing_key is either an opaque fingerprint or a URL. When it is a URL,
	// validate it with the same credential-safe URL contract while retaining
	// file:// support for local public-key files.
	if signingKey, _ := mc.Config["signing_key"].(string); strings.Contains(signingKey, "://") {
		checkURL := config.PlaceholderRe.ReplaceAllString(signingKey, "_")
		if err := artifact.ValidateURL(checkURL, []string{"http", "https", "file"}); err != nil {
			r.Add(ValidationError{Code: ErrMalformedURL, Field: fieldPath(toolName, methodIdx, "signing_key"), Message: fmt.Sprintf("%s: %v", mc.Kind, err)})
		}
	}

	for _, field := range contract.ArtifactFields {
		raw, _ := mc.Config[field].(string)
		if raw == "" {
			continue
		}
		if err := contract.ValidateArtifact(raw); err != nil {
			message := fmt.Sprintf("%s: %v", mc.Kind, err)
			var forbidden *artifact.ForbiddenExtensionError
			if errors.As(err, &forbidden) {
				message = fmt.Sprintf("%s method does not support platform installer artifact %s", mc.Kind, forbidden.Extension)
				if forbidden.Extension == ".msi" {
					message += "; use the msi method"
				} else {
					message += "; no dedicated installer method is available for this format"
				}
			}
			r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, methodIdx, field), Message: message})
		}
	}
}

// validateUnknownDistroFamily checks that when.distro_family values are
// known clan names.
func validateUnknownDistroFamily(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			if mc.When == nil {
				continue
			}
			for _, family := range mc.When.DistroFamily {
				if !knownDistroFamilies[family] {
					r.Add(ValidationError{
						Code:    WarnUnknownDistroFamily,
						Field:   fieldPath(toolName, i, "when.distro_family"),
						Message: fmt.Sprintf("unknown distro family %q (expected one of: %s)", family, knownDistroFamilyList()),
					})
				}
			}
		}
	}

	return r
}

// knownDistroFamilyList returns a comma-separated sorted list of known families.
func knownDistroFamilyList() string {
	families := make([]string, 0, len(knownDistroFamilies))
	for f := range knownDistroFamilies {
		families = append(families, f)
	}
	sort.Strings(families)
	return strings.Join(families, ", ")
}

// validateSignatureSecurity warns when signature_url is set without signing_key.
// Without a signing_key, GPGVerify cannot enforce signer identity, falling back
// to the shared keyring with no identity check — a security gap.
func validateSignatureSecurity(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			sigURL, hasSigURL := mc.Config["signature_url"]
			sigURLStr, sigURLIsStr := sigURL.(string)
			if !hasSigURL || !sigURLIsStr || sigURLStr == "" {
				continue
			}

			sigKey, hasSigKey := mc.Config["signing_key"]
			sigKeyStr, sigKeyIsStr := sigKey.(string)
			if !hasSigKey || !sigKeyIsStr || sigKeyStr == "" {
				r.Add(ValidationError{
					Code:    WarnSignatureNoKey,
					Field:   fieldPath(toolName, i, "signing_key"),
					Message: "signature_url is set without signing_key; verification will not check signer identity",
				})
			}
		}
	}

	return r
}

// validateContainerReferences rejects container identities that cannot be
// represented by the container adapter's canonical source + tag/digest model.
func validateContainerReferences(s *config.Schema) *Result {
	r := &Result{}
	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			if mc.Kind != "container" {
				continue
			}
			if source, _ := mc.Config["source"].(string); source != "" {
				if err := containerref.ValidateRepository(source); err != nil {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "source"), Message: err.Error()})
				}
			}
			tag, _ := mc.Config["tag"].(string)
			digest, _ := mc.Config["digest"].(string)
			if tag != "" {
				if err := containerref.ValidateTag(tag); err != nil {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "tag"), Message: err.Error()})
				}
			}
			if digest != "" {
				if err := containerref.ValidateDigest(digest); err != nil {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "digest"), Message: err.Error()})
				}
			}
			if tag != "" && digest != "" {
				r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "digest"), Message: "container tag and digest are mutually exclusive"})
			}
			if platform, _ := mc.Config["platform"].(string); platform != "" {
				if _, err := containerref.NormalizePlatform(platform); err != nil {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "platform"), Message: err.Error()})
				}
			}
		}
	}
	return r
}
