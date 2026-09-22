package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func ownedResourceFixture() []plan.OwnedResourceState {
	return []plan.OwnedResourceState{
		{
			Resource:   plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "pkg:compiler"},
			Ownership:  plan.OwnershipDepengine,
			Dependents: []string{"tool-a"},
		},
		{
			Resource:   plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"},
			Ownership:  plan.OwnershipDepengine,
			Dependents: []string{"tool-a", "tool-b"},
		},
	}
}

func TestOwnedResourcesPersistThroughStateAndSnapshot(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	want := ownedResourceFixture()
	st := &State{
		Version:        currentStateVersion,
		Tools:          map[string]ToolState{"tool-a": {Method: "native"}},
		OwnedResources: want,
	}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.OwnedResources, want) {
		t.Fatalf("owned resources = %#v, want %#v", loaded.OwnedResources, want)
	}

	info, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadSnapshot(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.OwnedResources, want) {
		t.Fatalf("snapshot owned resources = %#v, want %#v", snapshot.OwnedResources, want)
	}
}

func TestSaveRejectsNonCanonicalOwnedResourceSnapshot(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	resources := ownedResourceFixture()
	resources[0], resources[1] = resources[1], resources[0]
	st := &State{Version: currentStateVersion, Tools: map[string]ToolState{}, OwnedResources: resources}
	if err := Save(st); err == nil || !strings.Contains(err.Error(), "sorted and unique") {
		t.Fatalf("Save error = %v, want canonical-order rejection", err)
	}
}

func TestLoadRejectsDuplicateOwnedResourceSnapshot(t *testing.T) {
	td := t.TempDir()
	path := filepath.Join(td, "state.json")
	writeChecksummedStateForTest(t, path, State{
		Version: currentStateVersion,
		Tools:   map[string]ToolState{},
		OwnedResources: []plan.OwnedResourceState{
			{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}, Ownership: plan.OwnershipDepengine},
			{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}, Ownership: plan.OwnershipDepengine},
		},
	})
	if _, err := LoadFrom(path); err == nil || !strings.Contains(err.Error(), "sorted and unique") {
		t.Fatalf("LoadFrom error = %v, want duplicate ownership rejection", err)
	}
}

func TestLoadSnapshotVerifiesStateChecksum(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	st := &State{Version: currentStateVersion, Tools: map[string]ToolState{"tool-a": {Method: "native"}}, OwnedResources: ownedResourceFixture()}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	info, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"repo:stable"`, `"repo:tampered"`, 1))
	if err := os.WriteFile(info.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(info.Path); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("LoadSnapshot error = %v, want checksum mismatch", err)
	}
}

func TestStateRejectsCredentialBearingOwnedResourceOnSaveAndLoad(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	resource := plan.OwnedResourceState{
		Resource:  plan.ResourceIdentity{Kind: plan.ResourceOther, Key: "authorization: Bearer supersecret"},
		Ownership: plan.OwnershipDepengine,
	}
	st := &State{Version: currentStateVersion, Tools: map[string]ToolState{}, OwnedResources: []plan.OwnedResourceState{resource}}
	if err := Save(st); err == nil || !strings.Contains(err.Error(), "credential-bearing text") {
		t.Fatalf("Save error = %v, want credential rejection", err)
	}

	path := filepath.Join(td, "state.json")
	writeChecksummedStateForTest(t, path, State{
		Version:        currentStateVersion,
		Tools:          map[string]ToolState{},
		OwnedResources: []plan.OwnedResourceState{resource},
	})
	if _, err := LoadFrom(path); err == nil || !strings.Contains(err.Error(), "credential-bearing text") {
		t.Fatalf("LoadFrom error = %v, want credential rejection", err)
	}
}
