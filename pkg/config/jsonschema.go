package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/pkg/methodkind"
)

//go:generate go run ../../cmd/schema-gen -output ../../schema/depengine.schema.json

// GenerateJSONSchema returns the editor-facing schema generated from the same
// method contracts used by runtime validation.
func GenerateJSONSchema() ([]byte, error) {
	definitions := map[string]any{
		"condition":        conditionJSONSchema(),
		"defaults":         defaultsJSONSchema(),
		"manifestOptions":  manifestOptionsJSONSchema(),
		"postInstall":      postInstallJSONSchema(),
		"projectDocument":  documentJSONSchema("tools", false),
		"manifestDocument": documentJSONSchema("packages", true),
		"tool":             toolJSONSchema(),
	}
	for _, contract := range methodkind.Contracts {
		definitions[methodDefinition(contract.Kind)] = methodValueJSONSchema(&contract, false)
	}
	definitions["methodVariant"] = variantJSONSchema()
	definitions["bucketMethod"] = bucketMethodJSONSchema()

	root := map[string]any{
		"$schema":     "https://json-schema.org/draft-07/schema#",
		"$id":         "https://raw.githubusercontent.com/Khorea1/depengine/master/schema/depengine.schema.json",
		"title":       "depengine configuration",
		"description": "Schema for version 1 project schemas and personal manifests.",
		"oneOf": []any{
			map[string]any{"$ref": "#/definitions/projectDocument"},
			map[string]any{"$ref": "#/definitions/manifestDocument"},
		},
		"definitions": definitions,
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		return nil, fmt.Errorf("encode JSON schema: %w", err)
	}
	return out.Bytes(), nil
}

func documentJSONSchema(section string, manifest bool) map[string]any {
	properties := map[string]any{
		"schema_version": map[string]any{"const": 1, "description": "depengine grammar version."},
		"defaults":       map[string]any{"$ref": "#/definitions/defaults"},
		section: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"simple": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"additionalProperties": map[string]any{"$ref": "#/definitions/tool"},
		},
	}
	if manifest {
		properties["manifest"] = map[string]any{"$ref": "#/definitions/manifestOptions"}
	}
	return map[string]any{
		"type":                 "object",
		"required":             []string{"schema_version", section},
		"properties":           properties,
		"additionalProperties": false,
	}
}

func defaultsJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"manager":      map[string]any{"type": "string", "minLength": 1, "default": "native"},
			"aur_helper":   map[string]any{"type": "string", "minLength": 1, "default": "paru"},
			"method_order": stringArrayJSONSchema(),
			"arch_map":     stringMapJSONSchema(),
			"os_map":       stringMapJSONSchema(),
		},
		"additionalProperties": false,
	}
}

func manifestOptionsJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"allow_new_tools": map[string]any{"type": "boolean", "default": false},
		},
		"additionalProperties": false,
	}
}

func toolJSONSchema() map[string]any {
	properties := map[string]any{
		"requires":      stringArrayJSONSchema(),
		"requires_when": map[string]any{"type": "object", "additionalProperties": map[string]any{"$ref": "#/definitions/condition"}},
		"pre_install":   map[string]any{"type": "string"},
		"post_install":  map[string]any{"$ref": "#/definitions/postInstall"},
		"tags":          stringArrayJSONSchema(),
		"method_prefer": stringArrayJSONSchema(),
		"method_only":   stringArrayJSONSchema(),
	}
	for _, kind := range methodkind.KnownKinds() {
		contract, ok := methodkind.Lookup(kind)
		if ok {
			properties[kind] = map[string]any{"$ref": "#/definitions/" + methodDefinition(contract.Kind)}
		}
	}
	for bucket := range methodkind.DefaultBuckets {
		properties[bucket] = map[string]any{"$ref": "#/definitions/bucketMethod"}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": map[string]any{"$ref": "#/definitions/methodVariant"},
	}
}

func methodValueJSONSchema(contract *methodkind.Contract, includeVariants bool) map[string]any {
	options := make([]any, 0, 3)
	if contract.AllowString {
		options = append(options, map[string]any{"type": "string"})
	}
	if contract.AllowTrue {
		options = append(options, map[string]any{"const": true})
	}
	options = append(options, methodObjectJSONSchema(contract, ""))
	if includeVariants {
		options = append(options, map[string]any{"$ref": "#/definitions/methodVariant"})
	}
	return map[string]any{"oneOf": options}
}

func variantJSONSchema() map[string]any {
	kinds := methodkind.KnownKinds()
	options := make([]any, 0, len(kinds))
	for _, kind := range kinds {
		contract, _ := methodkind.Lookup(kind)
		options = append(options, methodObjectJSONSchema(contract, kind))
	}
	return map[string]any{"oneOf": options}
}

func bucketMethodJSONSchema() map[string]any {
	contract := methodkind.Contract{Fields: map[string]methodkind.Field{"pkg": {Type: methodkind.String}}, AllowString: true, AllowTrue: true}
	return methodValueJSONSchema(&contract, false)
}

func methodObjectJSONSchema(contract *methodkind.Contract, variantKind string) map[string]any {
	properties := map[string]any{
		"when":     map[string]any{"$ref": "#/definitions/condition"},
		"arch_map": stringMapJSONSchema(),
		"os_map":   stringMapJSONSchema(),
	}
	required := make([]string, 0, len(contract.Fields)+1)
	if variantKind != "" {
		properties["kind"] = map[string]any{"const": variantKind}
		required = append(required, "kind")
	}
	keys := make([]string, 0, len(contract.Fields))
	for key := range contract.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field := contract.Fields[key]
		properties[key] = methodFieldJSONSchema(field)
		if field.Required {
			required = append(required, key)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	if len(contract.MutuallyExclusive) > 0 {
		allOf := make([]any, 0, len(contract.MutuallyExclusive))
		for _, group := range contract.MutuallyExclusive {
			allOf = append(allOf, map[string]any{"not": map[string]any{"required": group}})
		}
		schema["allOf"] = allOf
	}
	return schema
}

func methodFieldJSONSchema(field methodkind.Field) map[string]any {
	var schema map[string]any
	switch field.Type {
	case methodkind.String:
		schema = map[string]any{"type": "string"}
	case methodkind.Boolean:
		schema = map[string]any{"type": "boolean"}
	case methodkind.Integer:
		schema = map[string]any{"type": "integer"}
	case methodkind.IntegerOrString:
		schema = map[string]any{"oneOf": []any{
			map[string]any{"type": "integer"},
			map[string]any{"type": "string", "pattern": "^[0-9]+$"},
		}}
	case methodkind.StringMap:
		schema = stringMapJSONSchema()
	default:
		panic("unknown method field type: " + field.Type)
	}
	if field.NonEmpty && field.Type == methodkind.String {
		schema["minLength"] = 1
	}
	if len(field.Enum) > 0 {
		schema["enum"] = field.Enum
	}
	return schema
}

func conditionJSONSchema() map[string]any {
	properties := make(map[string]any, len(conditionFields))
	for name, fieldType := range conditionFields {
		if fieldType == "bool" {
			properties[name] = map[string]any{"type": "boolean"}
		} else {
			properties[name] = stringArrayJSONSchema()
		}
	}
	return map[string]any{
		"type":                 "object",
		"minProperties":        1,
		"properties":           properties,
		"additionalProperties": false,
	}
}

func postInstallJSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "string"},
		map[string]any{
			"type":     "object",
			"required": []string{"cmd"},
			"properties": map[string]any{
				"cmd":  map[string]any{"type": "string"},
				"when": map[string]any{"$ref": "#/definitions/condition"},
			},
			"additionalProperties": false,
		},
	}}
}

func stringArrayJSONSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func stringMapJSONSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
}

func methodDefinition(kind string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_")
	return "method_" + replacer.Replace(kind)
}
