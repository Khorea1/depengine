package source

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/scoopruntime"
)

type sourceScoopRuntime struct {
	caps       scoopruntime.Capabilities
	buckets    []scoopruntime.Bucket
	listErr    error
	bucketRepo string
	addErr     error
	adds       int
	removes    int
}

func (r *sourceScoopRuntime) Capabilities() scoopruntime.Capabilities  { return r.caps }
func (*sourceScoopRuntime) Available(context.Context, run.Runner) bool { return true }
func (*sourceScoopRuntime) ObserveInstalled(context.Context, run.Runner, string, string) (scoopruntime.InstalledPackage, error) {
	return scoopruntime.InstalledPackage{}, nil
}
func (*sourceScoopRuntime) InstallResolved(context.Context, run.Runner, scoopruntime.InstallTarget) error {
	return nil
}
func (*sourceScoopRuntime) RemoveResolved(context.Context, run.Runner, scoopruntime.InstallTarget) error {
	return nil
}
func (r *sourceScoopRuntime) BucketList(context.Context, run.Runner) ([]scoopruntime.Bucket, error) {
	return r.buckets, r.listErr
}
func (r *sourceScoopRuntime) BucketAdd(context.Context, run.Runner, string, string, map[string]string, []string) error {
	r.adds++
	return r.addErr
}
func (r *sourceScoopRuntime) BucketRemove(context.Context, run.Runner, string) error {
	r.removes++
	return nil
}
func (r *sourceScoopRuntime) BucketRepository(context.Context, run.Runner, string) (string, error) {
	return r.bucketRepo, nil
}

func TestScoopBucketPresenceUsesSemanticRuntimeState(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	runtime := &sourceScoopRuntime{caps: scoopruntime.Capabilities{BucketRevisionLocation: true}, buckets: []scoopruntime.Bucket{{Name: "vendor", Location: "https://example.test/vendor"}}, bucketRepo: "/tmp/vendor"}
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte(revision)}}}
	manager := NewManagerWithScoopRuntime(runner, false, runtime)
	source := config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor/", Revision: revision}
	present, err := manager.Present(context.Background(), source)
	if err != nil || !present {
		t.Fatalf("Present() = (%t, %v), want semantic bucket present", present, err)
	}
	source.URL = "https://mirror.test/vendor"
	present, err = manager.Present(context.Background(), source)
	if present || err == nil || !strings.Contains(err.Error(), "different origin") {
		t.Fatalf("Present() = (%t, %v), want origin mismatch", present, err)
	}
}

func TestScoopBucketMutationCapabilitiesAreCheckedBeforeRuntime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		caps   scoopruntime.Capabilities
		source config.Source
	}{
		{name: "custom location", caps: scoopruntime.Capabilities{BucketSelection: true}, source: config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor"}},
		{name: "revision", caps: scoopruntime.Capabilities{BucketSelection: true}, source: config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor", Revision: "0123456789012345678901234567890123456789"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &sourceScoopRuntime{caps: tc.caps}
			runner := &scriptedRunner{}
			err := NewManagerWithScoopRuntime(runner, false, runtime).Add(context.Background(), tc.source)
			if err == nil {
				t.Fatal("unsupported bucket operation accepted")
			}
			if runtime.adds != 0 || len(runner.calls) != 0 {
				t.Fatalf("mutation occurred before capability rejection: runtime adds=%d runner calls=%v", runtime.adds, runner.calls)
			}
		})
	}
}

func TestScoopBucketRemovalCapabilityIsCheckedBeforeRuntime(t *testing.T) {
	runtime := &sourceScoopRuntime{caps: scoopruntime.Capabilities{}, buckets: []scoopruntime.Bucket{{Name: "vendor"}}}
	err := NewManagerWithScoopRuntime(&scriptedRunner{}, false, runtime).Remove(context.Background(), []config.Source{{Kind: "scoop-bucket", Name: "vendor"}})
	if err == nil {
		t.Fatal("unsupported bucket removal accepted")
	}
	if runtime.removes != 0 {
		t.Fatalf("bucket removals = %d, want none", runtime.removes)
	}
}

func TestScoopBucketRuntimeMutationFailureIsReturnedWithoutRetry(t *testing.T) {
	failure := errors.New("selected runtime failed")
	runtime := &sourceScoopRuntime{caps: scoopruntime.Capabilities{BucketSelection: true, BucketRevisionLocation: true}, addErr: failure}
	manager := NewManagerWithScoopRuntime(&scriptedRunner{}, false, runtime)
	err := manager.Add(context.Background(), config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor"})
	if !errors.Is(err, failure) {
		t.Fatalf("Add() error = %v, want selected runtime failure", err)
	}
	if runtime.adds != 1 {
		t.Fatalf("runtime mutation attempts = %d, want one", runtime.adds)
	}
}

func TestScoopBucketRevisionMismatchRollsBackSemantically(t *testing.T) {
	const revision = "0123456789012345678901234567890123456789"
	const actual = "1123456789012345678901234567890123456789"
	runtime := &sourceScoopRuntime{caps: scoopruntime.Capabilities{BucketSelection: true, BucketRevisionLocation: true, Removal: true}, bucketRepo: "/tmp/vendor"}
	runner := &scriptedRunner{outputs: []run.Result{{Stdout: []byte(actual)}}}
	source := config.Source{Kind: "scoop-bucket", Name: "vendor", URL: "https://example.test/vendor", Revision: revision}
	result, err := NewManagerWithScoopRuntime(runner, false, runtime).EnsureTracked(context.Background(), []config.Source{source})
	if err == nil || !strings.Contains(err.Error(), "revision mismatch") {
		t.Fatalf("EnsureTracked() error = %v, want revision mismatch", err)
	}
	if len(result.Added) != 0 || result.Unconfirmed == nil || *result.Unconfirmed != source {
		t.Fatalf("EnsureTracked() result = %+v, mismatched add must remain unconfirmed", result)
	}
	if runtime.adds != 1 || runtime.removes != 1 {
		t.Fatalf("semantic bucket mutations: add=%d remove=%d, want one compensating rollback", runtime.adds, runtime.removes)
	}
}
