package platform

import (
	"math/big"
	"strings"
	"unicode"
)

// CompareVersion compares host/version identifiers that are commonly emitted
// by operating systems (for example "22.04", "14.6.1", or "10.0.26100").
// It intentionally does not impose SemVer semantics: values are split into
// numeric and textual runs, numeric runs compare by integer value, textual
// runs compare case-insensitively, and missing trailing numeric zero runs are
// ignored. The result is -1, 0, or 1.
func CompareVersion(a, b string) int {
	at := versionTokens(a)
	bt := versionTokens(b)
	for len(at) > 0 && isZeroNumericToken(at[len(at)-1]) {
		at = at[:len(at)-1]
	}
	for len(bt) > 0 && isZeroNumericToken(bt[len(bt)-1]) {
		bt = bt[:len(bt)-1]
	}
	limit := len(at)
	if len(bt) > limit {
		limit = len(bt)
	}
	for i := 0; i < limit; i++ {
		if i >= len(at) {
			return -1
		}
		if i >= len(bt) {
			return 1
		}
		aTok, bTok := at[i], bt[i]
		if aTok.numeric && bTok.numeric {
			var ai, bi big.Int
			ai.SetString(aTok.value, 10)
			bi.SetString(bTok.value, 10)
			if cmp := ai.Cmp(&bi); cmp != 0 {
				return cmpSign(cmp)
			}
			continue
		}
		if aTok.numeric != bTok.numeric {
			// Numeric release segments sort after textual qualifiers. This makes
			// "24.04" greater than "24.rc" without claiming SemVer behavior.
			if aTok.numeric {
				return 1
			}
			return -1
		}
		if cmp := strings.Compare(strings.ToLower(aTok.value), strings.ToLower(bTok.value)); cmp != 0 {
			return cmpSign(cmp)
		}
	}
	return 0
}

type versionToken struct {
	value   string
	numeric bool
}

func versionTokens(s string) []versionToken {
	s = strings.TrimSpace(s)
	var out []versionToken
	start := -1
	kindNumeric := false
	flush := func(end int) {
		if start >= 0 {
			out = append(out, versionToken{value: s[start:end], numeric: kindNumeric})
			start = -1
		}
	}
	for i, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush(i)
			continue
		}
		numeric := unicode.IsDigit(r)
		if start < 0 {
			start = i
			kindNumeric = numeric
			continue
		}
		if numeric != kindNumeric {
			flush(i)
			start = i
			kindNumeric = numeric
		}
	}
	flush(len(s))
	return out
}

func isZeroNumericToken(t versionToken) bool {
	if !t.numeric {
		return false
	}
	for _, r := range t.value {
		if r != '0' {
			return false
		}
	}
	return true
}

func cmpSign(v int) int {
	if v < 0 {
		return -1
	}
	if v > 0 {
		return 1
	}
	return 0
}
