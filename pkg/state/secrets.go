package state

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ValidateNoSecrets rejects state payloads that contain obvious credential
// material. State is persistent project/host metadata and must never become a
// secret store. Authentication belongs in external credential helpers or
// future secret-reference mechanisms.
func ValidateNoSecrets(s *State) error {
	if s == nil {
		return nil
	}
	for toolName, tool := range s.Tools {
		if err := validateValueNoSecrets(tool.Config, "tools."+toolName+".config"); err != nil {
			return err
		}
	}
	return nil
}

var diagnosticSecretPattern = regexp.MustCompile(`(?i)(--(?:token|password|passwd|secret|auth-token|access-token|api-key|apikey))(?:=|\s+)([^\s]+)|\b(authorization|proxy-authorization|cookie|set-cookie)\s*:\s*[^\r\n]+`)

func validateValueNoSecrets(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			childPath := path + "." + key
			if sensitiveStateKey(key) {
				return fmt.Errorf("state: refusing to persist sensitive field %s", childPath)
			}
			if err := validateValueNoSecrets(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		// Command argv is commonly represented as []any after TOML decoding.
		// Inspect the joined form as well as each value so ["--token", "x"]
		// cannot bypass string-level detection.
		parts := make([]string, 0, len(v))
		allStrings := true
		for _, child := range v {
			part, ok := child.(string)
			if !ok {
				allStrings = false
				break
			}
			parts = append(parts, part)
		}
		if allStrings && diagnosticSecretPattern.MatchString(strings.Join(parts, " ")) {
			return fmt.Errorf("state: refusing to persist credential-bearing command at %s", path)
		}
		for i, child := range v {
			if err := validateValueNoSecrets(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		if hasURLCredentials(v) {
			return fmt.Errorf("state: refusing to persist URL credentials at %s", path)
		}
		if diagnosticSecretPattern.MatchString(v) {
			return fmt.Errorf("state: refusing to persist credential-bearing text at %s", path)
		}
	}
	return nil
}

func sensitiveStateKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), ".", "_"))
	switch normalized {
	case "token", "auth_token", "access_token", "password", "passwd", "secret", "api_key", "apikey", "authorization", "cookie":
		return true
	default:
		return false
	}
}

func hasURLCredentials(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.User != nil
}
