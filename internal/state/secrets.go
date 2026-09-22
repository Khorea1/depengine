package state

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Khorea1/depengine/internal/plan"
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
	for i, resource := range s.OwnedResources {
		path := fmt.Sprintf("owned_resources[%d]", i)
		if err := validateValueNoSecrets(resource.Resource.Key, path+".resource.key"); err != nil {
			return err
		}
		for j, dependent := range resource.Dependents {
			if err := validateValueNoSecrets(dependent, fmt.Sprintf("%s.dependents[%d]", path, j)); err != nil {
				return err
			}
		}
	}
	for key, preparationPlan := range s.PreparationPlans {
		path := "preparation_plans." + key
		if err := validateValueNoSecrets(key, "preparation_plans.key"); err != nil {
			return err
		}
		for i, operation := range preparationPlan.Probe {
			if err := validatePreparationOperationNoSecrets(operation, fmt.Sprintf("%s.probe[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, mutation := range preparationPlan.Prepare {
			mutationPath := fmt.Sprintf("%s.prepare[%d]", path, i)
			if err := validateValueNoSecrets(mutation.ID, mutationPath+".id"); err != nil {
				return err
			}
			if err := validateValueNoSecrets(mutation.Resource.Key, mutationPath+".resource.key"); err != nil {
				return err
			}
			if err := validatePreparationOperationNoSecrets(mutation.Apply, mutationPath+".apply"); err != nil {
				return err
			}
			if mutation.Rollback != nil {
				if err := validatePreparationOperationNoSecrets(*mutation.Rollback, mutationPath+".rollback"); err != nil {
					return err
				}
			}
		}
		for i, operation := range preparationPlan.Commit {
			if err := validatePreparationOperationNoSecrets(operation, fmt.Sprintf("%s.commit[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	for key, journal := range s.PreparationJournals {
		if err := validateValueNoSecrets(key, "preparation_journals.key"); err != nil {
			return err
		}
		for i, id := range journal.Applied {
			if err := validateValueNoSecrets(id, fmt.Sprintf("preparation_journals.applied[%d]", i)); err != nil {
				return err
			}
		}
		if err := validateValueNoSecrets(journal.Applying, "preparation_journals.applying"); err != nil {
			return err
		}
		for i, id := range journal.RollbackApplied {
			if err := validateValueNoSecrets(id, fmt.Sprintf("preparation_journals.rollback_applied[%d]", i)); err != nil {
				return err
			}
		}
		if err := validateValueNoSecrets(journal.RollbackApplying, "preparation_journals.rollback_applying"); err != nil {
			return err
		}
	}
	return nil
}

func validatePreparationOperationNoSecrets(operation plan.Operation, path string) error {
	if err := validateValueNoSecrets(operation.Kind, path+".kind"); err != nil {
		return err
	}
	if err := validateValueNoSecrets(operation.Description, path+".description"); err != nil {
		return err
	}
	return validateValueNoSecrets(operation.Command, path+".command")
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
	case []string:
		if diagnosticSecretPattern.MatchString(strings.Join(v, " ")) {
			return fmt.Errorf("state: refusing to persist credential-bearing command at %s", path)
		}
		for i, child := range v {
			if err := validateValueNoSecrets(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
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
