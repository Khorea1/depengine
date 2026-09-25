package platform

import (
	"bufio"
	"bytes"
	"strings"
	"unicode"
)

type osRelease struct {
	ID         string
	Name       string
	PrettyName string
	IDLike     string
	VersionID  string
}

func parseOSRelease(data []byte) osRelease {
	var out osRelease
	scanner := bufio.NewScanner(bytes.NewReader(bytes.ReplaceAll(data, []byte("\r"), nil)))
	for scanner.Scan() {
		line := scanner.Text()
		key, raw, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		switch key {
		case "ID":
			out.ID, _ = sanitizeOSReleaseValue(raw, false)
		case "NAME":
			out.Name, _ = sanitizeOSReleaseValue(raw, false)
		case "PRETTY_NAME":
			out.PrettyName, _ = sanitizeOSReleaseValue(raw, false)
		case "ID_LIKE":
			out.IDLike, _ = sanitizeOSReleaseValue(raw, false)
		case "VERSION_ID":
			out.VersionID, _ = sanitizeOSReleaseValue(raw, true)
		}
	}
	return out
}

func sanitizeOSReleaseValue(raw string, allowNumeric bool) (string, bool) {
	value := strings.Trim(raw, " \t")
	value = strings.ReplaceAll(value, `\n`, " ")
	if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
		value = value[1:]
	}
	if len(value) > 0 && (value[len(value)-1] == '\'' || value[len(value)-1] == '"') {
		value = value[:len(value)-1]
	}
	value = strings.Trim(value, " \t")
	if len(value) > 256 {
		value = value[:256]
	}
	if value == "" {
		return "", false
	}
	for _, r := range value {
		if r != '\t' && unicode.IsControl(r) {
			return "", false
		}
	}
	if !allowNumeric {
		allDigits := true
		for _, r := range value {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return "", false
		}
	}
	return value, true
}
