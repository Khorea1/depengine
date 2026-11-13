package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/methodkind"
)

//go:generate go run ../../cmd/schema-gen -output ../../schema/depengine.schema.json

// GenerateJSONSchema returns the editor-facing schema generated from the same
// method contracts used by runtime validation.
func GenerateJSONSchema() ([]byte, error) {
	definitions := map[string]any{
		"condition":        conditionJSONSchema(),
		"defaults":         defaultsJSONSchema(),
		"manifestOptions":  manifestOptionsJSONSchema(),
		"hook":             hookJSONSchema(),
		"secretReference":  secretReferenceJSONSchema(),
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
	sectionDescription := "Tools declared by name; a plain string is shorthand for `{ native = \"<name>\" }`."
	if manifest {
		sectionDescription = "Packages declared by name; a plain string is shorthand for a native package of the same name."
	}
	properties := map[string]any{
		"schema_version": map[string]any{"const": 1, "description": "depengine grammar version."},
		"defaults":       map[string]any{"$ref": "#/definitions/defaults"},
		section: map[string]any{
			"type":        "object",
			"description": sectionDescription,
			"properties": map[string]any{
				"simple": map[string]any{"type": "array", "description": "Bare package names for the inferred native manager.", "items": map[string]any{"type": "string"}},
			},
			"additionalProperties": map[string]any{"$ref": "#/definitions/tool"},
		},
	}
	if manifest {
		properties["manifest"] = map[string]any{"$ref": "#/definitions/manifestOptions", "description": "Operator-local controls for treating this file as a personal manifest."}
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
			"manager":       map[string]any{"type": "string", "minLength": 1, "default": "native", "description": "Native package manager used when the native method is selected (default native)."},
			"aur_helper":    map[string]any{"type": "string", "minLength": 1, "default": "paru", "description": "AUR helper the aur method drives (default paru)."},
			"method_prefer": map[string]any{"type": "array", "description": "Global method priority prefix; methods not listed stay available as fallbacks.", "items": map[string]any{"type": "string"}},
			"method_order":  map[string]any{"type": "array", "description": "Compatibility alias for method_prefer; set only one of the two.", "items": map[string]any{"type": "string"}}, // compatibility alias
			"arch_map":      map[string]any{"type": "object", "description": "Map host architecture to the package or asset architecture name.", "additionalProperties": map[string]any{"type": "string"}},
			"os_map":        map[string]any{"type": "object", "description": "Map host operating system to the package or asset OS name.", "additionalProperties": map[string]any{"type": "string"}},
		},
		"allOf": []any{
			map[string]any{"not": map[string]any{"required": []string{"method_prefer", "method_order"}}},
		},
		"additionalProperties": false,
	}
}

func manifestOptionsJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"allow_new_tools": map[string]any{"type": "boolean", "default": false, "description": "Let this manifest contribute tools the project schema does not declare."},
		},
		"additionalProperties": false,
	}
}

func toolJSONSchema() map[string]any {
	properties := map[string]any{
		"requires":        map[string]any{"type": "array", "description": "Tools that must be installed before this one; each name must also be declared.", "items": map[string]any{"type": "string"}},
		"requires_when":   map[string]any{"type": "object", "description": "Per-dependency conditions: install the named dependency only when its condition matches.", "additionalProperties": map[string]any{"$ref": "#/definitions/condition"}},
		"pre_install":     map[string]any{"$ref": "#/definitions/hook", "description": "Command that runs before the selected method is executed."},
		"post_install":    map[string]any{"$ref": "#/definitions/hook", "description": "Command that runs after the selected method succeeds."},
		"tags":            map[string]any{"type": "array", "description": "Labels selected by `depengine install --tag`.", "items": map[string]any{"type": "string"}},
		"method_prefer":   map[string]any{"type": "array", "description": "Per-tool method priority prefix; methods not listed stay available.", "items": map[string]any{"type": "string"}},
		"method_only":     map[string]any{"type": "array", "description": "Exclusive method list: only these methods are considered, in this order.", "items": map[string]any{"type": "string"}},
		"dependency_only": map[string]any{"type": "boolean", "default": false, "description": "Declare a virtual tool that groups dependencies and has no install method."},
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
		"when":         map[string]any{"$ref": "#/definitions/condition"},
		"arch_map":     stringMapJSONSchema(),
		"os_map":       stringMapJSONSchema(),
		"requires":     stringArrayJSONSchema(),
		"pre_install":  map[string]any{"$ref": "#/definitions/hook"},
		"post_install": map[string]any{"$ref": "#/definitions/hook"},
		"sources": map[string]any{
			"type": "array", "minItems": 1,
			"items": map[string]any{
				"type": "object", "required": []string{"kind", "name"},
				"properties": map[string]any{
					"kind":       map[string]any{"enum": []string{"apt-ppa", "dnf-copr", "scoop-bucket", "brew-tap"}},
					"name":       map[string]any{"type": "string", "minLength": 1},
					"url":        map[string]any{"type": "string", "minLength": 1},
					"revision":   map[string]any{"type": "string", "pattern": "^(?:[0-9a-f]{40}|[0-9a-f]{64})$", "description": "Full lowercase Git commit object ID; supported only with an explicit brew-tap or scoop-bucket URL."},
					"secret_ref": map[string]any{"$ref": "#/definitions/secretReference"},
				},
				"additionalProperties": false,
			},
		},
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
		if key == "asset" && contract.Kind == "github" {
			// The principal github asset accepts the same selector
			// shapes as distribution overrides. Every other method
			// kind keeps the contract's string-only asset.
			properties[key] = assetSelectorJSONSchema()
		}
		if field.Required {
			required = append(required, key)
		}
	}
	if len(contract.DistributableFields) > 0 {
		properties["distributions"] = distributionsJSONSchema(contract.DistributableFields)
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	description := methodkind.Describe(contract.Kind)
	if capabilities := methodkind.CapabilityNames(contract.Capabilities); len(capabilities) > 0 {
		capabilityText := "Method capabilities: " + strings.Join(capabilities, ", ") + "."
		description = strings.TrimSpace(description + " " + capabilityText)
		if contract.Capabilities&methodkind.CapabilityImmutableLock != 0 {
			// The capability only says a concrete identity mechanism exists.
			// depengine.lock pins far less than that; never imply otherwise in
			// editor-facing text (see docs/support-boundary.md).
			description += " immutable-lock means a pinnable identity exists; depengine.lock currently pins latest-release references, checksums, local digests, mutable Git branch/tag commits (including cargo --git sources), container tag digests, and versions of plain unversioned npm, pnpm, and Yarn Classic packages."
		}
	}
	if description != "" {
		schema["description"] = description
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	allOf := make([]any, 0, len(contract.MutuallyExclusive)+len(contract.Requires))
	for _, group := range contract.MutuallyExclusive {
		allOf = append(allOf, map[string]any{"not": map[string]any{"required": group}})
	}
	if len(contract.Requires) > 0 {
		keys := make([]string, 0, len(contract.Requires))
		for key := range contract.Requires {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			allOf = append(allOf, map[string]any{
				"if":   map[string]any{"required": []string{key}},
				"then": map[string]any{"required": contract.Requires[key]},
			})
		}
	}
	if len(allOf) > 0 {
		schema["allOf"] = allOf
	}
	if len(contract.SourceAlternatives) > 0 {
		oneOf := make([]any, len(contract.SourceAlternatives))
		for i, alternative := range contract.SourceAlternatives {
			otherFields := make([]any, 0)
			for j, other := range contract.SourceAlternatives {
				if i == j {
					continue
				}
				for _, field := range other {
					otherFields = append(otherFields, map[string]any{"required": []string{field}})
				}
			}
			option := map[string]any{"required": alternative}
			if len(otherFields) > 0 {
				option["not"] = map[string]any{"anyOf": otherFields}
			}
			oneOf[i] = option
		}
		schema["oneOf"] = oneOf
	}
	return schema
}

// assetSelectorJSONSchema describes the shared asset-selector shape used
// both by the principal github asset field and by distribution asset
// overrides: a non-empty literal template string, or exactly
// { regex = "..." }. Keep this as the single definition of the selector
// format so the schema, strict validation, and the resolver agree.
func assetSelectorJSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{map[string]any{"type": "string", "minLength": 1}, map[string]any{"type": "object", "required": []string{"regex"}, "properties": map[string]any{"regex": map[string]any{"type": "string", "minLength": 1}}, "additionalProperties": false}}}
}

// distributionsJSONSchema describes the per-method distributions array. The
// allowed override keys come from the method contract's DistributableFields
// (single source); when is always required.
func distributionsJSONSchema(distributable []string) map[string]any {
	allowed := map[string]bool{"when": true}
	for _, key := range distributable {
		allowed[key] = true
	}
	properties := map[string]any{
		"when":                 map[string]any{"$ref": "#/definitions/condition"},
		"asset":                assetSelectorJSONSchema(),
		"checksum":             map[string]any{"type": "string", "minLength": 1},
		"checksum_url":         map[string]any{"type": "string", "minLength": 1},
		"checksum_file_format": map[string]any{"type": "string", "minLength": 1},
		"signature_url":        map[string]any{"type": "string", "minLength": 1},
		"binary":               map[string]any{"type": "string", "minLength": 1},
		"extract_to":           map[string]any{"type": "string", "minLength": 1},
		"strip_components":     map[string]any{"type": "integer", "minimum": 0},
		"entrypoints":          stringMapJSONSchema(),
	}
	names := make([]string, 0, len(properties))
	for key := range properties {
		if allowed[key] {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	filtered := make(map[string]any, len(names))
	for _, key := range names {
		filtered[key] = properties[key]
	}
	return map[string]any{
		"type": "array", "minItems": 1,
		"items": map[string]any{
			"type": "object", "required": []string{"when"},
			"properties":           filtered,
			"additionalProperties": false,
		},
	}
}

func secretReferenceJSONSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"provider", "name"},
		"properties": map[string]any{
			"provider": map[string]any{"type": "string", "minLength": 1, "pattern": `^[^\s\u0000](?:[^\u0000]*[^\s\u0000])?$`, "description": "Secret provider; env is the supported provider."},
			"name":     map[string]any{"type": "string", "minLength": 1, "pattern": `^[^\s\u0000](?:[^\u0000]*[^\s\u0000])?$`, "description": "Provider-specific secret name, for example an environment variable."},
		},
		"additionalProperties": false,
	}
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
	case methodkind.StringList:
		schema = stringArrayJSONSchema()
	case methodkind.StringStringMap:
		schema = stringMapJSONSchema()
	case methodkind.SecretRef:
		schema = map[string]any{"$ref": "#/definitions/secretReference"}
	case methodkind.Command:
		command := map[string]any{
			"type":                 "object",
			"required":             []string{"run"},
			"properties":           map[string]any{"run": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}}},
			"additionalProperties": false,
		}
		schema = map[string]any{"oneOf": []any{
			map[string]any{"type": "string", "minLength": 1},
			command,
			map[string]any{"type": "array", "minItems": 1, "items": command},
		}}
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

// conditionFieldNotes documents each condition dimension for editors and for
// `depengine explain`. The semantics live in docs/schema-reference.md.
var conditionFieldNotes = map[string]string{
	"distro_family":      "Resolved distro clan such as debian or arch, not the raw distro ID.",
	"distro_id":          "Raw distro ID such as ubuntu or fedora; use distro_family for the clan.",
	"distro_version":     "Exact normalized distro version; numeric runs ignore leading zeroes.",
	"distro_version_min": "Inclusive minimum distro version.",
	"distro_version_max": "Inclusive maximum distro version.",
	"arch":               "Host CPU architecture.",
	"os":                 "Host operating system; Termux reports linux, so use is_android there.",
	"target_family":      "Portable family: unix (linux, darwin, BSDs, termux) or windows.",
	"kernel":             "Exact kernel release, not a range; use kernel_min/kernel_max for ranges.",
	"kernel_min":         "Inclusive minimum kernel version.",
	"kernel_max":         "Inclusive maximum kernel version.",
	"libc":               "libc family token (glibc, musl), matched by prefix with no version.",
	"init_system":        "Init system such as systemd, openrc, runit, or sysvinit.",
	"is_wsl":             "Three-state WSL fact: true needs a positive detection, false a conclusive negative.",
	"is_container":       "Three-state container fact; same rule as is_wsl.",
	"is_android":         "Three-state Termux/Android fact, detected from Termux environment variables.",
}

func conditionJSONSchema() map[string]any {
	properties := make(map[string]any, len(conditionFields))
	for name, fieldType := range conditionFields {
		var property map[string]any
		switch fieldType {
		case "bool":
			property = map[string]any{"type": "boolean"}
		case "string":
			property = map[string]any{"type": "string", "minLength": 1}
		default:
			property = stringArrayJSONSchema()
		}
		if note := conditionFieldNotes[name]; note != "" {
			property["description"] = note
		}
		properties[name] = property
	}
	return map[string]any{
		"type":                 "object",
		"minProperties":        1,
		"properties":           properties,
		"additionalProperties": false,
	}
}

func hookJSONSchema() map[string]any {
	command := map[string]any{
		"type": "object",
		"oneOf": []any{
			map[string]any{"required": []string{"cmd"}},
			map[string]any{"required": []string{"run"}},
		},
		"properties": map[string]any{
			"cmd":  map[string]any{"type": "string", "minLength": 1, "description": "Shell string; a shell interprets it."},
			"run":  map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": "argv form: the first element is the executable and no shell is involved."},
			"when": map[string]any{"$ref": "#/definitions/condition", "description": "Run the hook only when this condition matches."},
		},
		"additionalProperties": false,
	}
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "string", "minLength": 1},
		command,
		map[string]any{"type": "array", "minItems": 1, "items": command},
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
