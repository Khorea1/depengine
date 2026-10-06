package state

import "testing"

func TestToolStateArchivePayloadOwnedSupportsTypedAndLegacyState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state ToolState
		want  bool
	}{
		{name: "typed", state: ToolState{OwnsArchivePayload: true}, want: true},
		{name: "legacy config", state: ToolState{Config: map[string]any{"_http_owned_archive_payload": true}}, want: true},
		{name: "absent", state: ToolState{Config: map[string]any{}}, want: false},
		{name: "legacy false", state: ToolState{Config: map[string]any{"_http_owned_archive_payload": false}}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.ArchivePayloadOwned(); got != tc.want {
				t.Fatalf("ArchivePayloadOwned() = %t, want %t", got, tc.want)
			}
		})
	}
}
