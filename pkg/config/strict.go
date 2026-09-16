package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/pkg/methodkind"
)

var conditionFields = map[string]string{
	"distro_family": "strings",
	"target_family": "strings",
	"distro_id":     "strings",
	"arch":          "strings",
	"os":            "strings",
	"kernel":        "strings",
	"libc":          "strings",
	"init_system":   "strings",
	"is_wsl":        "bool",
	"is_container":  "bool",
	"is_android":    "bool",
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
	for _, key := range sortedMapKeys(m) {
		path := "defaults." + key
		switch key {
		case "manager", "aur_helper":
			validateNonEmptyString(m[key], path, errs)
		case "method_order":
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
	default:
		*errs = append(*errs, fmt.Sprintf("%s: expected string, true, or table, got %T", path, raw))
	}
}

func validateMethodField(raw any, field methodkind.Field, path string, errs *[]string) {
	switch field.Type {
	case methodkind.String:
		validateString(raw, path, errs)
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
