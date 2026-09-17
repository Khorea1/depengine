package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

// validateRequiredFields applies the central methodkind contracts to each
// normalized candidate. Adapter-specific semantic checks stay below.
func validateRequiredFields(s *config.Schema) *Result {
	r := &Result{}
	for toolName, tool := range s.Tools {
		for i, method := range tool.Methods {
			contract, ok := methodkind.Lookup(method.Kind)
			if !ok {
				continue
			}
			keys := make([]string, 0, len(contract.Fields))
			for key := range contract.Fields {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				field := contract.Fields[key]
				value, present := method.Config[key]
				if !present {
					if field.Required {
						r.Add(ValidationError{Code: ErrRequiredField, Field: fieldPath(toolName, i, key), Message: fmt.Sprintf("%s method for tool %q requires a %s field", contract.Kind, toolName, key)})
					}
					continue
				}
				if !methodFieldTypeMatches(value, field.Type) {
					r.Add(ValidationError{Code: ErrRequiredField, Field: fieldPath(toolName, i, key), Message: fmt.Sprintf("%s must be %s, got %T", key, fieldTypeDescription(field.Type), value)})
					continue
				}
				if field.NonEmpty {
					if value, ok := value.(string); ok && value == "" {
						r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, key), Message: fmt.Sprintf("%s method for tool %q: %s must not be empty", contract.Kind, toolName, key)})
					}
				}
				if len(field.Enum) > 0 {
					value, _ := value.(string)
					if !containsString(field.Enum, value) {
						r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, key), Message: fmt.Sprintf("%s method %s must be one of %q, got %q", contract.Kind, key, field.Enum, value)})
					}
				}
			}
			configKeys := make([]string, 0, len(method.Config))
			for key := range method.Config {
				configKeys = append(configKeys, key)
			}
			sort.Strings(configKeys)
			for _, key := range configKeys {
				if strings.HasPrefix(key, "_") {
					continue
				}
				if _, ok := contract.Fields[key]; !ok {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, key), Message: fmt.Sprintf("field %s is not supported by method kind %s", key, contract.Kind)})
				}
			}
			for _, group := range contract.MutuallyExclusive {
				var present []string
				for _, key := range group {
					if _, ok := method.Config[key]; ok {
						present = append(present, key)
					}
				}
				if len(present) > 1 {
					r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, present[0]), Message: fmt.Sprintf("%s method for tool %q sets mutually exclusive fields %s", contract.Kind, toolName, strings.Join(present, " and "))})
				}
			}
			if contract.Kind == "http" || contract.Kind == "github" || contract.Kind == "appimage" || contract.Kind == "android" || contract.Kind == "msi" {
				_, hasURL := method.Config["url"]
				_, hasRepo := method.Config["repo"]
				_, hasAsset := method.Config["asset"]
				if contract.Kind == "github" {
					hasRepo = true
				}
				if hasURL == hasRepo || hasRepo != hasAsset {
					field := "url"
					if hasRepo || hasAsset {
						field = "artifact"
					}
					r.Add(ValidationError{Code: ErrRequiredField, Field: fieldPath(toolName, i, field), Message: fmt.Sprintf("%s method for tool %q requires exactly one of url or repo+asset", contract.Kind, toolName)})
				}
			}
			if strip, ok := method.Config["strip_components"].(int64); ok && strip < 0 {
				r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, i, "strip_components"), Message: "strip_components must be non-negative"})
			}
			if method.Kind == "git" {
				validateManagedPaths(toolName, i, method.Config["managed_paths"], r)
			}
			validateChecksum(toolName, i, method, r)
		}
	}
	return r
}

func validateManagedPaths(tool string, index int, raw any, result *Result) {
	values, ok := raw.([]any)
	if !ok {
		return
	}
	home, _ := os.UserHomeDir()
	shared := map[string]bool{"/": true, "/bin": true, "/sbin": true, "/usr": true, "/usr/bin": true, "/usr/local": true, "/usr/local/bin": true, "/opt": true}
	for _, rawPath := range values {
		path, ok := rawPath.(string)
		if !ok {
			continue
		}
		path = filepath.Clean(config.ExpandHomeDir(path))
		if !filepath.IsAbs(path) || path == filepath.Dir(path) || path == filepath.Clean(home) || shared[path] {
			result.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(tool, index, "managed_paths"), Message: fmt.Sprintf("unsafe managed path %q: expected an absolute, exact owned target", path)})
		}
	}
}

func methodFieldTypeMatches(value any, fieldType methodkind.FieldType) bool {
	switch fieldType {
	case methodkind.String:
		_, ok := value.(string)
		return ok
	case methodkind.Boolean:
		_, ok := value.(bool)
		return ok
	case methodkind.Integer:
		_, ok := value.(int64)
		return ok
	case methodkind.IntegerOrString:
		switch value := value.(type) {
		case int, int64:
			return true
		case string:
			_, err := strconv.Atoi(value)
			return err == nil
		}
	case methodkind.StringMap:
		values, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	case methodkind.StringList:
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	case methodkind.StringStringMap:
		values, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	case methodkind.Command:
		return commandTypeMatches(value)
	}
	return false
}

func commandTypeMatches(value any) bool {
	if command, ok := value.(string); ok {
		return command != ""
	}
	if commands, ok := value.([]any); ok {
		if len(commands) == 0 {
			return false
		}
		for _, command := range commands {
			if !commandTableTypeMatches(command) {
				return false
			}
		}
		return true
	}
	return commandTableTypeMatches(value)
}

func commandTableTypeMatches(value any) bool {
	table, ok := value.(map[string]any)
	if !ok || len(table) != 1 {
		return false
	}
	run, ok := table["run"].([]any)
	if !ok || len(run) == 0 {
		return false
	}
	for _, arg := range run {
		if _, ok := arg.(string); !ok {
			return false
		}
	}
	executable, _ := run[0].(string)
	return executable != ""
}

func fieldTypeDescription(fieldType methodkind.FieldType) string {
	switch fieldType {
	case methodkind.String:
		return "a string"
	case methodkind.Boolean:
		return "a boolean"
	case methodkind.Integer:
		return "an integer"
	case methodkind.IntegerOrString:
		return "an integer or numeric string"
	case methodkind.StringMap:
		return "a table of strings"
	case methodkind.Command:
		return "a string, command table, or command table list"
	default:
		return string(fieldType)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateChecksum(toolName string, methodIdx int, method *config.MethodCandidate, r *Result) {
	if method.Kind != "http" {
		return
	}
	checksum, ok := method.Config["checksum"].(string)
	if !ok || checksum == "" {
		return
	}
	if strings.HasSuffix(checksum, ":auto") {
		r.Add(ValidationError{Code: WarnAutoChecksum, Field: fieldPath(toolName, methodIdx, "checksum"), Message: fmt.Sprintf("checksum %q uses :auto — TOFU (Trust On First Use) applies, hash is NOT verified", checksum)})
		return
	}
	algorithms := []struct {
		prefix string
		length int
	}{{"sha256:", 64}, {"sha512:", 128}, {"sha1:", 40}, {"md5:", 32}}
	for _, algorithm := range algorithms {
		if !strings.HasPrefix(checksum, algorithm.prefix) {
			continue
		}
		hexPart := checksum[len(algorithm.prefix):]
		if len(hexPart) != algorithm.length || !isHexString(hexPart) {
			r.Add(ValidationError{Code: ErrInvalidChecksum, Field: fieldPath(toolName, methodIdx, "checksum"), Message: fmt.Sprintf("checksum %q has invalid format: expected %d hex characters after %s", checksum, algorithm.length, algorithm.prefix)})
		}
		return
	}
	r.Add(ValidationError{Code: ErrInvalidValue, Field: fieldPath(toolName, methodIdx, "checksum"), Message: fmt.Sprintf("checksum %q does not use a recognized prefix (sha256:, sha512:, sha1:, md5:)", checksum)})
}

// validateWhenDirectives checks that when clauses only use known keys.
// If When is non-nil but IsZero() is true, the parser didn't recognize any keys.
func validateWhenDirectives(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		for i, mc := range tool.Methods {
			if mc.When == nil {
				continue
			}

			// If When is non-nil but all fields are zero, it means the
			// when clause had keys the parser didn't recognize.
			if mc.When.IsZero() {
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
func validatePlaceholders(s *config.Schema) *Result {
	r := &Result{}

	for toolName, tool := range s.Tools {
		// Check requires entries.
		for _, dep := range tool.Requires {
			scanPlaceholders(dep, fieldPath(toolName, -1, "requires"), r)
		}

		for _, set := range []struct {
			name  string
			hooks []config.Hook
		}{{"pre_install", tool.PreInstall}, {"post_install", tool.PostInstall}} {
			for _, hook := range set.hooks {
				for _, arg := range hook.Run {
					scanPlaceholders(arg, fieldPath(toolName, -1, set.name), r)
				}
			}
		}

		// Check method configs.
		for i, mc := range tool.Methods {
			for key, val := range mc.Config {
				strVal, ok := val.(string)
				if !ok || strVal == "" {
					continue
				}
				field := fieldPath(toolName, i, key)
				if key != "asset" {
					for _, token := range []string{"version", "arch_any", "os_any"} {
						if strings.Contains(strVal, "{"+token+"}") {
							r.Add(ValidationError{Code: WarnUnknownPlaceholder, Field: field, Message: fmt.Sprintf("placeholder {%s} is only valid in a repo+asset pattern", token)})
						}
					}
				}
				scanPlaceholders(strVal, field, r)
			}
		}
	}

	return r
}

// scanPlaceholders extracts {name} tokens from s and flags unknown ones.
func scanPlaceholders(s, field string, r *Result) {
	matches := config.PlaceholderRe.FindAllStringSubmatch(s, -1)
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

// validateMethodOrderConflicts checks that no tool has both method_prefer
// and method_only set simultaneously.
func validateMethodOrderConflicts(s *config.Schema) *Result {
	r := &Result{}
	for name, tool := range s.Tools {
		if len(tool.MethodPrefer) > 0 && len(tool.MethodOnly) > 0 {
			r.Add(ValidationError{
				Code:    ErrInvalidValue,
				Field:   fmt.Sprintf("tools.%s", name),
				Message: fmt.Sprintf("method_prefer and method_only cannot both be set on tool %q", name),
			})
		}
	}
	return r
}
