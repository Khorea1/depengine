package main

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

func TestLockPinForToolStateUsesPersistedLabel(t *testing.T) {
	primary := &config.MethodCandidate{Kind: "http", Label: "primary"}
	mirror := &config.MethodCandidate{Kind: "http", Label: "mirror"}
	tool := &config.Tool{Methods: []*config.MethodCandidate{primary, mirror}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	pin, ok := lockPinForToolState(lk, "demo", tool, state.ToolState{Method: "mirror", MethodKind: "http"})
	if !ok || pin.Latest != "v2" {
		t.Fatalf("lockPinForToolState = %+v, %v; want v2, true", pin, ok)
	}
}

func TestLockPinForToolStateFailsClosedForAmbiguousLegacyState(t *testing.T) {
	tool := &config.Tool{Methods: []*config.MethodCandidate{
		{Kind: "http", Label: "primary"},
		{Kind: "http", Label: "mirror"},
	}}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{
		"demo/http/0": {Latest: "v1"},
		"demo/http/1": {Latest: "v2"},
	}}

	if pin, ok := lockPinForToolState(lk, "demo", tool, state.ToolState{Method: "http", MethodKind: "http"}); ok {
		t.Fatalf("lockPinForToolState accepted ambiguous legacy state: %+v", pin)
	}
}
