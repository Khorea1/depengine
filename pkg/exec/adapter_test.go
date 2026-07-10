package exec

import (
	"context"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
)

type removableMockAdapter struct {
	mockAdapter
}

func (r *removableMockAdapter) Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	return nil
}

func (r *removableMockAdapter) CanRemove() bool { return true }

type removableFalseMockAdapter struct {
	mockAdapter
}

func (r *removableFalseMockAdapter) Remove(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	return nil
}

func (r *removableFalseMockAdapter) CanRemove() bool { return false }

func TestCanRemoveReturnsTrueForRemover(t *testing.T) {
	a := &removableMockAdapter{mockAdapter{kindValue: "removable"}}
	if !CanRemove(a) {
		t.Fatal("CanRemove should be true for adapter implementing Remover")
	}
}

func TestCanRemoveReturnsFalseWhenRemoverSaysFalse(t *testing.T) {
	a := &removableFalseMockAdapter{mockAdapter{kindValue: "removable-false"}}
	if CanRemove(a) {
		t.Fatal("CanRemove should be false when adapter.Remover.CanRemove() returns false")
	}
}

func TestCanRemoveReturnsFalseForNonRemover(t *testing.T) {
	a := &mockAdapter{kindValue: "non-removable"}
	if CanRemove(a) {
		t.Fatal("CanRemove should be false for adapter without Remover")
	}
}
