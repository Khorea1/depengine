package contracttest_test

import (
	"testing"

	"github.com/Khorea1/depengine/internal/contracttest"
	"github.com/Khorea1/depengine/internal/methodkind"
)

func TestBehavioralCoverageMatchesDeclaredFieldEffects(t *testing.T) {
	declared := make(map[string]methodkind.FieldEffect)
	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			declared[contract.Kind+"."+name] = field.Effects
		}
	}

	phases := []struct {
		phase   contracttest.Phase
		effect  methodkind.FieldEffect
		require bool
	}{
		{contracttest.PhaseResolveRuntime, methodkind.EffectResolve, false},
		{contracttest.PhaseExecute, methodkind.EffectExecute, true},
		{contracttest.PhaseVerify, methodkind.EffectVerify, true},
	}

	for _, item := range phases {
		entries := contracttest.CoverageKeys(item.phase)
		for key := range entries {
			effects, ok := declared[key]
			if !ok {
				t.Errorf("%s coverage %q is orphaned: no such contract field", item.phase, key)
				continue
			}
			if effects&item.effect == 0 {
				t.Errorf("%s coverage %q is orphaned: field no longer declares the matching effect", item.phase, key)
			}
		}
		if !item.require {
			continue
		}
		for key, effects := range declared {
			if effects&item.effect == 0 {
				continue
			}
			if _, ok := entries[key]; !ok {
				t.Errorf("%s declares %s but has no behavioral evidence", key, item.phase)
			}
		}
	}
}
