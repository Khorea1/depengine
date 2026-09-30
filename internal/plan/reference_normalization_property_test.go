package plan

import "testing"

// FuzzSanitizeLockReferenceCanonicalizesValidIdentity pins the persistence
// boundary used by lock projection and identity comparison: once a reference
// has passed identity validation, sanitization must preserve validity and reach
// a fixed point after one pass. Otherwise repeated lock projection/encoding
// could change identity bytes without any manifest change.
func FuzzSanitizeLockReferenceCanonicalizesValidIdentity(f *testing.F) {
	for _, seed := range []string{
		"https://Example.TEST/path?z=2&a=1#section",
		"ssh://git@Example.TEST/org/repo.git",
		"git@Example.TEST:org/repo.git",
		"HTTPS://[FE80::1%25eth0]/repo?z=2&a=1",
		"conda-forge",
		"file:///tmp/tool",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 2048 {
			return
		}
		if err := validateIdentityReference(raw); err != nil {
			return
		}

		canonical := sanitizeLockReference(raw)
		if err := validateIdentityReference(canonical); err != nil {
			t.Fatalf("sanitized valid reference became invalid: raw=%q canonical=%q err=%v", raw, canonical, err)
		}
		if again := sanitizeLockReference(canonical); again != canonical {
			t.Fatalf("sanitizeLockReference is not a fixed point: raw=%q canonical=%q second=%q", raw, canonical, again)
		}
	})
}
