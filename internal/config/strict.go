package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/platform"
)

var conditionFields = map[string]string{
	"distro_family":      "strings",
	"target_family":      "strings",
	"distro_id":          "strings",
	"distro_version":     "strings",
	"distro_version_min": "string",
	"distro_version_max": "string",
	"arch":               "strings",
	"os":                 "strings",
	"kernel":             "strings",
	"libc":               "strings",
	"init_system":        "strings",
	"is_wsl":             "bool",
	"is_container":       "bool",
	"is_android":         "bool",
}

func validateRawSchema(raw map[string]any, section string) error {
	allowedRoot := map[string]bool{"schema_version": true, "defaults": true, section: true}
	if section == "packages" {
		allowedRoot["manifest"] = true
	}

	var errs []string
	for _, key := range sortedMapKeys(raw) {
		if !allowedRoot[key] {
			errs = append(errs, fmt.Sprintf("%s: unknown root field", key))
		}
	}
	validateSchemaVersion(raw["schema_version"], &errs)
	validateDefaults(raw["defaults"], &errs)
	if _, ok := raw[section]; !ok {
		errs = append(errs, section+": required table")
	} else {
		validateToolSection(raw[section], section, &errs)
	}
	if section == "packages" {
		validateManifest(raw["manifest"], &errs)
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid schema:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

func validateSchemaVersion(raw any, errs *[]string) {
	if raw == nil {
		*errs = append(*errs, "schema_version: required field")
		return
	}
	version, ok := raw.(int64)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("schema_version: expected integer, got %T", raw))
		return
	}
	if version != 1 {
		*errs = append(*errs, fmt.Sprintf("schema_version: unsupported version %d (supported: 1)", version))
	}
}

func validateDefaults(raw any, errs *[]string) {
	if raw == nil {
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("defaults: expected table, got %T", raw))
		return
	}
	if _, hasPrefer := m["method_prefer"]; hasPrefer {
		if _, hasOrder := m["method_order"]; hasOrder {
			*errs = append(*errs, "defaults: method_prefer and method_order are mutually exclusive; use method_prefer")
		}
	}
	for _, key := range sortedMapKeys(m) {
		path := "defaults." + key
		switch key {
		case "manager", "aur_helper":
			validateNonEmptyString(m[key], path, errs)
		case "method_prefer", "method_order":
			validateStringList(m[key], path, errs)
		case "arch_map", "os_map":
			validateStringMap(m[key], path, errs)
		default:
			*errs = append(*errs, path+": unknown field")
		}
	}
}

func validateToolSection(raw any, section string, errs *[]string) {
	if raw == nil {
		return
	}
	tools, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", section, raw))
		return
	}
	for _, name := range sortedMapKeys(tools) {
		path := section + "." + name
		if name == "simple" {
			validateStringList(tools[name], path, errs)
			continue
		}
		tool, ok := tools[name].(map[string]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", path, tools[name]))
			continue
		}
		validateTool(tool, path, errs)
	}
}

func validateTool(tool map[string]any, path string, errs *[]string) {
	var requires map[string]bool
	if raw, ok := tool["requires"]; ok {
		if values, valid := stringList(raw, path+".requires", errs); valid {
			requires = make(map[string]bool, len(values))
			for _, dep := range values {
				requires[dep] = true
			}
		}
	}

	for _, key := range sortedMapKeys(tool) {
		fieldPath := path + "." + key
		switch key {
		case "requires":
		case "dependency_only":
			if _, ok := tool[key].(bool); !ok {
				*errs = append(*errs, fmt.Sprintf("%s: expected boolean, got %T", fieldPath, tool[key]))
			}
		case "requires_when":
			validateRequiresWhen(tool[key], fieldPath, requires, errs)
		case "pre_install", "post_install":
			validateHooks(tool[key], fieldPath, errs)
		case "tags", "method_prefer", "method_only":
			validateStringList(tool[key], fieldPath, errs)
		case "preinstall":
			*errs = append(*errs, fieldPath+": unsupported field; use pre_install")
		case "postinstall":
			*errs = append(*errs, fieldPath+": unsupported field; use post_install")
		case "method_order":
			*errs = append(*errs, fieldPath+": unsupported field; use method_prefer or method_only")
		case "when", "kind":
			*errs = append(*errs, fieldPath+": field is only valid inside a method table")
		default:
			validateMethodValue(tool[key], fieldPath, key, errs)
		}
	}
}

func validateRequiresWhen(raw any, path string, requires map[string]bool, errs *[]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", path, raw))
		return
	}
	for _, dep := range sortedMapKeys(m) {
		depPath := path + "." + dep
		if !requires[dep] {
			*errs = append(*errs, depPath+": dependency must also appear in requires")
		}
		validateCondition(m[dep], depPath, errs)
	}
}

func validateHooks(raw any, path string, errs *[]string) {
	switch v := raw.(type) {
	case string:
		validateNonEmptyString(v, path, errs)
	case map[string]any:
		validateHook(v, path, errs)
	case []any:
		if len(v) == 0 {
			*errs = append(*errs, path+": must not be empty")
		}
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				*errs = append(*errs, fmt.Sprintf("%s[%d]: expected table, got %T", path, i, item))
				continue
			}
			validateHook(m, fmt.Sprintf("%s[%d]", path, i), errs)
		}
	default:
		*errs = append(*errs, fmt.Sprintf("%s: expected string, table, or array of tables, got %T", path, raw))
	}
}

func validateHook(m map[string]any, path string, errs *[]string) {
	for _, key := range sortedMapKeys(m) {
		switch key {
		case "cmd":
			validateNonEmptyString(m[key], path+".cmd", errs)
		case "run":
			if values, valid := stringList(m[key], path+".run", errs); valid {
				if len(values) == 0 {
					*errs = append(*errs, path+".run: must not be empty")
				} else if values[0] == "" {
					*errs = append(*errs, path+".run[0]: executable must not be empty")
				}
			}
		case "when":
			validateCondition(m[key], path+".when", errs)
		default:
			*errs = append(*errs, path+"."+key+": unknown field")
		}
	}
	_, hasCmd := m["cmd"]
	_, hasRun := m["run"]
	if hasCmd == hasRun {
		*errs = append(*errs, path+": exactly one of cmd or run is required")
	}
}

func validateMethodValue(raw any, path, declaredKind string, errs *[]string) {
	contract, known := methodkind.Lookup(declaredKind)
	switch v := raw.(type) {
	case string:
		if known && !contract.AllowString {
			*errs = append(*errs, path+": this method requires a config table")
		}
	case bool:
		if !v {
			*errs = append(*errs, path+": false is not a valid method value")
		} else if known && !contract.AllowTrue {
			*errs = append(*errs, path+": this method requires a config table")
		}
	case map[string]any:
		if kind, ok := v["kind"].(string); ok && kind != "" {
			_, isBucket := methodkind.DefaultBuckets[declaredKind]
			if methodkind.IsKnownKind(declaredKind) || isBucket {
				*errs = append(*errs, path+".kind: kind override requires a custom method label")
			}
			contract, known = methodkind.Lookup(kind)
		}
		for _, key := range sortedMapKeys(v) {
			switch key {
			case "kind":
				validateNonEmptyString(v[key], path+".kind", errs)
			case "when":
				validateCondition(v[key], path+".when", errs)
			case "arch_map", "os_map":
				validateStringMap(v[key], path+"."+key, errs)
			case "requires":
				validateStringList(v[key], path+".requires", errs)
			case "sources":
				validateSources(v[key], path+".sources", errs)
			default:
				if !known {
					continue
				}
				field, ok := contract.Fields[key]
				if !ok {
					*errs = append(*errs, path+"."+key+": field is not supported by method kind "+contract.Kind)
					continue
				}
				validateMethodField(v[key], field, path+"."+key, errs)
			}
		}
		if known {
			validateRequiredMethodFields(v, path, contract, errs)
			validateArtifactChoice(v, path, contract, errs)
			for _, group := range contract.MutuallyExclusive {
				present := 0
				for _, key := range group {
					if _, ok := v[key]; ok {
						present++
					}
				}
				if present > 1 {
					*errs = append(*errs, path+": fields "+strings.Join(group, " and ")+" are mutually exclusive")
				}
			}
			for key, required := range contract.Requires {
				if _, configured := v[key]; !configured {
					continue
				}
				for _, dependency := range required {
					if _, ok := v[dependency]; !ok {
						*errs = append(*errs, path+"."+key+": requires "+dependency)
					}
				}
			}
		}
	default:
		*errs = append(*errs, fmt.Sprintf("%s: expected string, true, or table, got %T", path, raw))
	}
}

func validateRequiredMethodFields(v map[string]any, path string, contract *methodkind.Contract, errs *[]string) {
	var required []string
	for key, field := range contract.Fields {
		if field.Required {
			required = append(required, key)
		}
	}
	sort.Strings(required)
	for _, key := range required {
		if _, configured := v[key]; !configured {
			*errs = append(*errs, path+"."+key+": required field")
		}
	}
}

// validateArtifactChoice enforces the shared url XOR repo+asset source choice
// for every contract that declares an artifact model, so new artifact kinds
// inherit it instead of being added to a method-name list.
func validateArtifactChoice(v map[string]any, path string, contract *methodkind.Contract, errs *[]string) {
	if contract.Artifact == nil {
		return
	}
	_, hasURL := v["url"]
	_, hasRepo := v["repo"]
	_, hasAsset := v["asset"]
	if _, acceptsURL := contract.Fields["url"]; !acceptsURL {
		// Repo-only kinds (github) always take the repo+asset alternative.
		hasRepo = true
	}
	if hasURL == hasRepo {
		*errs = append(*errs, path+": exactly one of url or repo+asset is required")
	}
	if hasRepo != hasAsset {
		*errs = append(*errs, path+": repo and asset must be specified together")
	}
	if strip, ok := v["strip_components"].(int64); ok && strip < 0 {
		*errs = append(*errs, path+".strip_components: must be non-negative")
	}
}

func validateMethodField(raw any, field methodkind.Field, path string, errs *[]string) {
	switch field.Type {
	case methodkind.String:
		if field.NonEmpty {
			validateNonEmptyString(raw, path, errs)
		} else {
			validateString(raw, path, errs)
		}
	case methodkind.Boolean:
		if _, ok := raw.(bool); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected boolean, got %T", path, raw))
		}
	case methodkind.Integer:
		if _, ok := raw.(int64); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected integer, got %T", path, raw))
		}
	case methodkind.IntegerOrString:
		if _, ok := raw.(int64); ok {
			return
		}
		if value, ok := raw.(string); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected integer or numeric string, got %T", path, raw))
		} else if _, err := strconv.Atoi(value); err != nil {
			*errs = append(*errs, path+": expected integer or numeric string")
		}
	case methodkind.StringMap:
		validateStringMap(raw, path, errs)
	case methodkind.Command:
		validateCommand(raw, path, errs)
	case methodkind.StringList:
		validateStringList(raw, path, errs)
	case methodkind.StringStringMap:
		validateStringMap(raw, path, errs)
	}
	if len(field.Enum) > 0 {
		if value, ok := raw.(string); ok && !containsString(field.Enum, value) {
			*errs = append(*errs, fmt.Sprintf("%s: expected one of %s", path, strings.Join(field.Enum, ", ")))
		}
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

func validateSources(raw any, path string, errs *[]string) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		*errs = append(*errs, path+": expected non-empty array of tables")
		return
	}
	for i, item := range items {
		p := fmt.Sprintf("%s[%d]", path, i)
		m, ok := item.(map[string]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", p, item))
			continue
		}
		for _, key := range sortedMapKeys(m) {
			if key != "kind" && key != "name" && key != "url" && key != "secret_ref" {
				*errs = append(*errs, p+"."+key+": unknown field")
			}
		}
		validateNonEmptyString(m["kind"], p+".kind", errs)
		validateNonEmptyString(m["name"], p+".name", errs)
		kind, _ := m["kind"].(string)
		if kind != "apt-ppa" && kind != "dnf-copr" && kind != "scoop-bucket" && kind != "brew-tap" {
			*errs = append(*errs, p+".kind: unknown source kind")
		}
		if rawURL, exists := m["url"]; exists {
			validateNonEmptyString(rawURL, p+".url", errs)
			if kind == "apt-ppa" || kind == "dnf-copr" {
				*errs = append(*errs, p+".url: unsupported for source kind "+kind)
			}
		}
		if rawRef, exists := m["secret_ref"]; exists {
			validateSecretReference(rawRef, p+".secret_ref", errs)
		}
	}
}

func validateSecretReference(raw any, path string, errs *[]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", path, raw))
		return
	}
	for _, key := range sortedMapKeys(m) {
		if key != "provider" && key != "name" {
			*errs = append(*errs, path+"."+key+": unknown field")
		}
	}
	for _, key := range []string{"provider", "name"} {
		field := path + "." + key
		validateNonEmptyString(m[key], field, errs)
		if value, ok := m[key].(string); ok && (strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00')) {
			*errs = append(*errs, field+": must not contain surrounding whitespace or NUL")
		}
	}
}

func validateCommand(raw any, path string, errs *[]string) {
	if command, ok := raw.(string); ok {
		validateNonEmptyString(command, path, errs)
		return
	}
	if commands, ok := raw.([]any); ok {
		if len(commands) == 0 {
			*errs = append(*errs, path+": must not be empty")
		}
		for i, command := range commands {
			validateCommandTable(command, fmt.Sprintf("%s[%d]", path, i), errs)
		}
		return
	}
	validateCommandTable(raw, path, errs)
}

func validateCommandTable(raw any, path string, errs *[]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected command table, got %T", path, raw))
		return
	}
	for _, key := range sortedMapKeys(m) {
		if key != "run" {
			*errs = append(*errs, path+"."+key+": unknown field")
		}
	}
	values, valid := stringList(m["run"], path+".run", errs)
	if valid && (len(values) == 0 || values[0] == "") {
		*errs = append(*errs, path+".run: executable must not be empty")
	}
}

func validateCondition(raw any, path string, errs *[]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected table, got %T", path, raw))
		return
	}
	meaningful := false
	for _, key := range sortedMapKeys(m) {
		fieldPath := path + "." + key
		switch conditionFields[key] {
		case "strings":
			if values, valid := stringList(m[key], fieldPath, errs); valid && len(values) > 0 {
				meaningful = true
			}
		case "string":
			if value, ok := m[key].(string); !ok {
				*errs = append(*errs, fmt.Sprintf("%s: expected string, got %T", fieldPath, m[key]))
			} else if strings.TrimSpace(value) == "" {
				*errs = append(*errs, fieldPath+": must not be empty")
			} else {
				meaningful = true
			}
		case "bool":
			if _, ok := m[key].(bool); !ok {
				*errs = append(*errs, fmt.Sprintf("%s: expected boolean, got %T", fieldPath, m[key]))
			} else {
				meaningful = true
			}
		default:
			*errs = append(*errs, fieldPath+": unknown condition field")
		}
	}
	if min, minOK := m["distro_version_min"].(string); minOK && strings.TrimSpace(min) != "" {
		if max, maxOK := m["distro_version_max"].(string); maxOK && strings.TrimSpace(max) != "" && platform.CompareVersion(min, max) > 0 {
			*errs = append(*errs, path+": distro_version_min must not be greater than distro_version_max")
		}
	}
	if !meaningful {
		*errs = append(*errs, path+": condition must not be empty")
	}
}

func validateManifest(raw any, errs *[]string) {
	if raw == nil {
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("manifest: expected table, got %T", raw))
		return
	}
	for _, key := range sortedMapKeys(m) {
		if key != "allow_new_tools" {
			*errs = append(*errs, "manifest."+key+": unknown field")
			continue
		}
		if _, ok := m[key].(bool); !ok {
			*errs = append(*errs, fmt.Sprintf("manifest.allow_new_tools: expected boolean, got %T", m[key]))
		}
	}
}

func validateString(raw any, path string, errs *[]string) {
	if _, ok := raw.(string); !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected string, got %T", path, raw))
	}
}

func validateNonEmptyString(raw any, path string, errs *[]string) {
	v, ok := raw.(string)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected string, got %T", path, raw))
	} else if v == "" {
		*errs = append(*errs, path+": must not be empty")
	}
}

func validateStringList(raw any, path string, errs *[]string) {
	stringList(raw, path, errs)
}

func stringList(raw any, path string, errs *[]string) ([]string, bool) {
	values, ok := raw.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected array of strings, got %T", path, raw))
		return nil, false
	}
	out := make([]string, 0, len(values))
	valid := true
	for i, value := range values {
		s, ok := value.(string)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s[%d]: expected string, got %T", path, i, value))
			valid = false
			continue
		}
		out = append(out, s)
	}
	return out, valid
}

func validateStringMap(raw any, path string, errs *[]string) {
	m, ok := raw.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected table of strings, got %T", path, raw))
		return
	}
	for _, key := range sortedMapKeys(m) {
		validateString(m[key], path+"."+key, errs)
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
