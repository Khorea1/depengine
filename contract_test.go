package main

import (
	"sort"
	"testing"

	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/methodkind"
)

func TestRegisteredAdaptersMatchMethodContracts(t *testing.T) {
	registered := exec.RegisteredKinds()
	sort.Strings(registered)
	registeredSet := make(map[string]bool, len(registered))
	for _, kind := range registered {
		registeredSet[kind] = true
		contract, ok := methodkind.Lookup(kind)
		if !ok {
			t.Errorf("registered adapter %q has no method contract", kind)
			continue
		}
		if got := exec.CanRemove(exec.Lookup(kind)); got != contract.CanRemove {
			t.Errorf("adapter %q CanRemove=%t, contract=%t", kind, got, contract.CanRemove)
		}
	}
	for _, contract := range methodkind.Contracts {
		if !registeredSet[contract.Kind] {
			t.Errorf("method contract %q has no registered adapter", contract.Kind)
		}
	}
}
