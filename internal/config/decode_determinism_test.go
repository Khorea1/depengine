package config

import (
	"reflect"
	"testing"
)

func TestDecodeStructFieldsSortsLeftoverKeys(t *testing.T) {
	type target struct {
		Known []string `cfg:"known"`
	}
	var dst target
	got := decodeStructFields(&dst, map[string]any{
		"zeta":  true,
		"known": "value",
		"alpha": true,
		"mid":   true,
	})
	want := []string{"alpha", "mid", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leftover = %v, want %v", got, want)
	}
}
