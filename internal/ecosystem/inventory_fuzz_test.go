package ecosystem

import (
	"context"
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// FuzzPipxInventoryObservation exercises hostile, truncated and inconsistent
// JSON emitted by an external package manager. Invalid process results must
// never be converted to authoritative absence or mutation approval.
func FuzzPipxInventoryObservation(f *testing.F) {
	for _, data := range []string{
		`{"pipx_spec_version":"0.1","venvs":{}}`,
		`{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":"1.0"}}}}}`,
		`{"pipx_spec_version":"0.1","venvs":{"demo":{"metadata":{"main_package":{"package":"demo","package_version":""}}}}}`,
		`{"pipx_spec_version":"0.1","venvs":`,
		`{"venvs":{"demo":null}}`,
		`{"venvs":{}} {"venvs":{"demo":{}}}`,
		"\x00\xff\xfe",
		"",
	} {
		f.Add([]byte(data), uint8(0))
		f.Add([]byte(data), uint8(2))
	}
	f.Fuzz(func(t *testing.T, output []byte, mode uint8) {
		if len(output) > 32<<10 {
			return
		}
		rn := &run.FakeRunner{Stdout: string(output), LookPaths: map[string]bool{"pipx": true}}
		switch mode % 5 {
		case 1:
			rn.ExitCode = 1
		case 2:
			rn.WaitErr = errors.New("output capture interrupted")
		case 3:
			rn.Err = context.Canceled
		case 4:
			rn.Stderr = "inconsistent inventory"
			rn.ExitCode = 2
		}
		method := &config.MethodCandidate{Kind: "pipx", Config: map[string]any{"pkg": "demo", "version": "1.0"}}
		observed, err := NewBaseAdapter(Configs["pipx"]).Observe(context.Background(), rn, &config.Tool{Name: "demo"}, method)
		if err != nil {
			return // an explicit adapter error cannot authorize a mutation
		}
		if mode%5 != 0 && (observed.Presence == plan.PresencePresent || observed.Presence == plan.PresenceAbsent) {
			t.Fatalf("failed or incomplete external process authorized presence=%q", observed.Presence)
		}
		result := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.0"}, observed)
		if err := result.Validate(); err != nil {
			t.Fatalf("invalid reconciliation: %v", err)
		}
		decision, transitionErr := plan.TransitionForVerification(result)
		if result.State == plan.StateUnknown || result.State == plan.StateBroken {
			if transitionErr == nil || decision.Required {
				t.Fatalf("uncertain JSON selected mutation: %+v, %v", decision, transitionErr)
			}
		}
	})
}

// FuzzSnapInventoryObservation covers malformed manager tables, unexpected
// versions, duplicate rows and partial process output without invoking snap.
func FuzzSnapInventoryObservation(f *testing.F) {
	for _, text := range []string{
		"Name Version Rev Tracking Publisher Notes\n",
		"Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 latest/stable store -\n",
		"Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 latest/stable store -\ndemo 2.0 43 latest/stable store -\n",
		"Name Version Rev Tracking Publisher Notes\ndemo 1.0 42 latest/stable store -\ninvalid\n",
		"demo 1.0 42 latest/stable store -\n",
		"\x00\xffgarbage\n",
		"",
	} {
		f.Add([]byte(text), uint8(0))
		f.Add([]byte(text), uint8(1))
	}
	f.Fuzz(func(t *testing.T, output []byte, mode uint8) {
		if len(output) > 32<<10 {
			return
		}
		rn := &run.FakeRunner{Stdout: string(output), LookPaths: map[string]bool{"snap": true}}
		switch mode % 4 {
		case 1:
			rn.WaitErr = errors.New("incomplete capture")
		case 2:
			rn.Err = context.DeadlineExceeded
		case 3:
			rn.ExitCode = 1
		}
		method := &config.MethodCandidate{Kind: "snap", Config: map[string]any{"pkg": "demo"}}
		obs, err := NewBaseAdapter(Configs["snap"]).Observe(context.Background(), rn, &config.Tool{Name: "demo"}, method)
		if err != nil {
			return
		}
		if mode%4 != 0 && (obs.Presence == plan.PresencePresent || obs.Presence == plan.PresenceAbsent) {
			t.Fatalf("failed or incomplete snap process authorized %q", obs.Presence)
		}
		verification := plan.Reconcile(plan.ResolvedIdentity{Package: "demo", Version: "1.0"}, obs)
		decision, transitionErr := plan.TransitionForVerification(verification)
		if verification.State == plan.StateUnknown || verification.State == plan.StateBroken {
			if transitionErr == nil || decision.Required {
				t.Fatalf("untrusted inventory selected mutation: %+v, %v", decision, transitionErr)
			}
		}
	})
}
