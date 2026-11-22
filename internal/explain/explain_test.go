package explain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/validate"
	"github.com/pelletier/go-toml/v2"
)

func mustCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return catalog
}

func TestCatalogAnswersEveryDocumentedTerm(t *testing.T) {
	catalog := mustCatalog(t)
	for _, entry := range catalog.Entries() {
		if entry.Term == "" || entry.Group == "" || entry.Summary == "" || entry.Form == "" || entry.Doc == "" {
			t.Errorf("incomplete entry %+v", entry)
		}
	}
}

func TestCatalogCoversEveryMethodKind(t *testing.T) {
	catalog := mustCatalog(t)
	for _, kind := range methodkind.KnownKinds() {
		matches := catalog.Lookup(kind)
		found := false
		for _, entry := range matches {
			if entry.Group == GroupMethod && entry.Term == kind {
				found = true
				if entry.Summary == "" {
					t.Errorf("method %q has no definition", kind)
				}
			}
		}
		if !found {
			t.Errorf("method %q missing from the catalog", kind)
		}
	}
}

func TestCatalogExplainsEverySchemaField(t *testing.T) {
	catalog := mustCatalog(t)
	raw, err := config.GenerateJSONSchema()
	if err != nil {
		t.Fatalf("GenerateJSONSchema: %v", err)
	}
	var doc schemaDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	for _, section := range fieldSections {
		definition := doc.Definitions[section.definition]
		for name := range definition.Properties {
			if section.definition == "tool" && (methodkind.IsKnownKind(name) || isBucket(name)) {
				continue
			}
			found := false
			for _, entry := range catalog.Lookup(name) {
				if entry.Group == GroupField && entry.Where == section.where {
					found = true
				}
			}
			if !found {
				t.Errorf("field %s.%s missing from the catalog", section.definition, name)
			}
		}
	}
}

func TestCatalogExplainsErrorVocabulary(t *testing.T) {
	catalog := mustCatalog(t)
	codes := []validate.ErrorCode{
		validate.ErrRequiredField, validate.ErrDanglingRef, validate.ErrCycle,
		validate.ErrMalformedURL, validate.ErrInvalidValue, validate.ErrInvalidChecksum,
		validate.ErrUnsafePackageName, validate.ErrDupeTool, validate.ErrSchemaNotFound,
		validate.WarnUnknownPlaceholder, validate.WarnUnknownWhenKey,
		validate.WarnUnknownDistroFamily, validate.WarnAutoChecksum, validate.WarnEnvMissing,
		validate.WarnInertTool, validate.WarnAmbiguousCondition,
	}
	for _, code := range codes {
		matches := catalog.Lookup(string(code))
		if len(matches) == 0 || matches[0].Group != GroupErrorCode {
			t.Errorf("error code %s missing from the catalog", code)
		}
	}
	classes := []plan.ErrorClass{
		plan.ErrorInvalidManifest, plan.ErrorUnsupportedCapability, plan.ErrorUnavailableCandidate,
		plan.ErrorResolutionFailure, plan.ErrorAuthRequirement, plan.ErrorHostIncompatibility,
	}
	for _, class := range classes {
		matches := catalog.Lookup(string(class))
		if len(matches) == 0 || matches[0].Group != GroupErrorClass {
			t.Errorf("resolver error class %s missing from the catalog", class)
		}
	}
}

// TestExamplesAreValidTOML guards the promise that every minimal example can
// be pasted into a schema.
func TestExamplesAreValidTOML(t *testing.T) {
	catalog := mustCatalog(t)
	for _, entry := range catalog.Entries() {
		if entry.Example == "" {
			continue
		}
		var document string
		switch {
		case entry.Group == GroupMethod:
			document = entry.Example
		case entry.Where == "[tools.<name>]":
			document = "[tools.myapp]\n" + entry.Example
		case entry.Where == "[defaults]":
			document = "[defaults]\n" + entry.Example
		case entry.Where == "[manifest]":
			document = "[manifest]\n" + entry.Example
		case strings.HasPrefix(entry.Where, "inside a when"):
			document = "[when]\n" + entry.Example
		default:
			document = entry.Example
		}
		var decoded map[string]any
		if err := toml.Unmarshal([]byte(document), &decoded); err != nil {
			t.Errorf("%s example is not valid TOML: %v\n%s", entry.Term, err, document)
		}
	}
}

func TestLookupIsCaseInsensitiveAndSuggests(t *testing.T) {
	catalog := mustCatalog(t)
	if matches := catalog.Lookup("HTTP"); len(matches) == 0 || matches[0].Term != "http" {
		t.Fatalf("case-insensitive lookup failed: %+v", matches)
	}
	if matches := catalog.Lookup("e_dupe_tool"); len(matches) == 0 {
		t.Fatal("lowercase error code lookup failed")
	}
	if matches := catalog.Lookup("reques_when"); len(matches) != 0 {
		t.Fatalf("typo unexpectedly resolved: %+v", matches)
	}
	suggestions := catalog.Suggest("reques_when")
	if len(suggestions) == 0 || suggestions[0] != "requires_when" {
		t.Fatalf("suggestions = %v, want requires_when first", suggestions)
	}
}

func TestLookupReturnsEveryContextForSharedNames(t *testing.T) {
	catalog := mustCatalog(t)
	matches := catalog.Lookup("method_prefer")
	if len(matches) != 2 {
		t.Fatalf("method_prefer matches = %d, want 2", len(matches))
	}
	var tool, defaults bool
	for _, entry := range matches {
		switch entry.Where {
		case "[tools.<name>]":
			tool = true
		case "[defaults]":
			defaults = true
		}
	}
	if !tool || !defaults {
		t.Fatalf("method_prefer contexts missing: %+v", matches)
	}
}
