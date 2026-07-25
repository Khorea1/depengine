package validate

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/graph"
	"github.com/Khorea1/depengine/pkg/config"
)

// validateCycles detects dependency cycles using graph.Sort.
func validateCycles(s *config.Schema) *Result {
	r := &Result{}

	_, err := graph.Sort(s.Tools)
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
		for _, dep := range tool.Requires {
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

// validateMalformedURLs checks URL fields for basic syntactic validity.
func validateMalformedURLs(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			var urlStr string
			switch mc.Kind {
			case "git", "http":
				if v, ok := mc.Config["url"]; ok {
					urlStr, _ = v.(string)
				}
			}
			if urlStr == "" {
				continue
			}

			// Replace placeholders with a safe token so URL parsing
			// doesn't choke on {latest} etc.
			checkURL := config.PlaceholderRe.ReplaceAllString(urlStr, "_")

			parsed, err := url.Parse(checkURL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				r.Add(ValidationError{
					Code:    ErrMalformedURL,
					Field:   fieldPath(toolName, i, "url"),
					Message: fmt.Sprintf("malformed URL %q", urlStr),
				})
			}
		}
	}

	return r
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
