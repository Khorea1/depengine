package exec

import (
	"context"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	depstate "github.com/Khorea1/depengine/internal/state"
)

func TestToolStateForResultPersistsArchiveOwnership(t *testing.T) {
	ex := New()
	tool := &config.Tool{Name: "demo"}
	result := ToolResult{
		Tool:               "demo",
		Status:             StatusInstalled,
		Method:             "http",
		MethodKind:         "http",
		OwnsArchivePayload: true,
		Config:             map[string]any{"url": "https://example.test/demo.zip"},
	}
	got := ex.toolStateForResult(context.Background(), tool, result, depstate.ToolState{}, false, true)
	if !got.OwnsArchivePayload {
		t.Fatal("archive ownership was not persisted")
	}
	if _, legacy := got.Config["_http_owned_archive_payload"]; legacy {
		t.Fatal("typed archive ownership leaked back into Config")
	}
}

func TestToolStateForResultMigratesLegacyArchiveOwnershipOnSatisfiedInstall(t *testing.T) {
	ex := New()
	tool := &config.Tool{Name: "demo"}
	existing := depstate.ToolState{
		Method:     "http",
		MethodKind: "http",
		Config: map[string]any{
			"url":                         "https://example.test/demo.zip",
			"_http_owned_archive_payload": true,
		},
	}
	result := ToolResult{
		Tool:       "demo",
		Status:     StatusAlready,
		Method:     "http",
		MethodKind: "http",
		Config:     map[string]any{"url": "https://example.test/demo.zip"},
	}
	got := ex.toolStateForResult(context.Background(), tool, result, existing, true, true)
	if !got.OwnsArchivePayload {
		t.Fatal("legacy archive ownership was not migrated to the typed state field")
	}
	if _, legacy := got.Config["_http_owned_archive_payload"]; legacy {
		t.Fatal("legacy archive ownership key survived migration")
	}
}
