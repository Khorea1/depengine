package config

import "reflect"

// decodeStructFields populates dst (a pointer to a struct) from raw using
// each field's `cfg` struct tag as the TOML key to read. This replaces the
// hand-written per-field type switch that used to live in parseCondition:
// every field the struct wants to accept is declared once, on the field
// itself, instead of once in the struct and again in a decode function.
//
// Supported destination field kinds: []string (via toStringSlice, so bool/
// string/[]any leaves all normalize the same way) and *bool (via
// parseBoolPtr). Unrecognized value shapes for a matched key leave the
// field at its zero value, exactly as the old switch-based code did.
//
// A field with no `cfg` tag, or tag "-", is never populated. Keys in raw
// that don't match any tag are returned in leftover so callers can log or
// otherwise report them (mirrors the old "invalidKeys" collection).
func decodeStructFields(dst any, raw map[string]any) (leftover []string) {
	v := reflect.ValueOf(dst).Elem()
	t := v.Type()

	consumed := make(map[string]bool, len(raw))
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("cfg")
		if tag == "" || tag == "-" {
			continue
		}
		val, ok := raw[tag]
		if !ok {
			continue
		}
		consumed[tag] = true

		fv := v.Field(i)
		switch fv.Interface().(type) {
		case []string:
			fv.Set(reflect.ValueOf(toStringSlice(val)))
		case *bool:
			fv.Set(reflect.ValueOf(parseBoolPtr(val)))
		}
	}

	for k := range raw {
		if !consumed[k] {
			leftover = append(leftover, k)
		}
	}
	return leftover
}
