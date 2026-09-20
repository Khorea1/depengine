package run

import (
	"regexp"
	"strings"
)

var sensitiveQueryKeys = []string{
	"token",
	"access_token",
	"auth_token",
	"refresh_token",
	"id_token",
	"api_key",
	"apikey",
	"password",
	"passwd",
	"secret",
	"client_secret",
	"credential",
	"signature",
	"sig",
	"x-amz-signature",
	"x-amz-credential",
	"x-amz-security-token",
	"x-goog-signature",
	"x-goog-credential",
}

var sensitiveFlagNames = []string{
	"--token",
	"--password",
	"--passwd",
	"--secret",
	"--client-secret",
	"--auth-token",
	"--access-token",
	"--refresh-token",
	"--api-key",
	"--apikey",
}

var (
	urlUserinfoPattern     = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	sensitiveHeaderPattern = regexp.MustCompile(`(?im)\b(authorization|proxy-authorization|cookie|set-cookie)\s*:\s*[^\r\n]+`)
	secretFlagPattern      = regexp.MustCompile(`(?i)(` + strings.Join(sensitiveFlagNames, "|") + `)(?:=|\s+)([^\s]+)`)
	secretQueryPattern     = regexp.MustCompile(`(?i)([?&#](?:` + strings.Join(sensitiveQueryKeys, "|") + `)=)[^&#\s]+`)
)

// IsSensitiveQueryKey reports whether a URL query parameter conventionally
// carries credential/signature material that must not be persisted or logged.
// Keeping this classification next to diagnostic redaction prevents the lock
// and plan validators from drifting to a weaker secret vocabulary.
func IsSensitiveQueryKey(key string) bool {
	key = strings.ToLower(key)
	for _, sensitive := range sensitiveQueryKeys {
		if key == sensitive {
			return true
		}
	}
	return false
}

// IsSensitiveFlag reports whether a CLI flag conventionally carries a secret
// value in either --flag=value or --flag value form.
func IsSensitiveFlag(flag string) bool {
	flag = strings.ToLower(flag)
	for _, sensitive := range sensitiveFlagNames {
		if flag == sensitive {
			return true
		}
	}
	return false
}

// RedactSensitiveText removes common credential forms from diagnostic text.
// It is intended for logs and user-facing error messages, not for command
// execution. Adapters must still avoid placing secrets in argv whenever
// possible; this function is the final persistence/display boundary.
func RedactSensitiveText(s string) string {
	s = urlUserinfoPattern.ReplaceAllString(s, `${1}***@`)
	s = sensitiveHeaderPattern.ReplaceAllString(s, `${1}: ***`)
	s = secretFlagPattern.ReplaceAllString(s, `${1}=***`)
	s = secretQueryPattern.ReplaceAllString(s, `${1}***`)
	return s
}

// RedactError returns an error with credential-safe display text while
// preserving the original error in its unwrap chain.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return &redactedWrappedError{
		message: RedactSensitiveText(err.Error()),
		cause:   err,
	}
}
