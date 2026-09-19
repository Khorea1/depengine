package run

import "regexp"

var (
	urlUserinfoPattern     = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	sensitiveHeaderPattern = regexp.MustCompile(`(?im)\b(authorization|proxy-authorization|cookie|set-cookie)\s*:\s*[^\r\n]+`)
	secretFlagPattern      = regexp.MustCompile(`(?i)(--(?:token|password|passwd|secret|auth-token|access-token|api-key|apikey))(?:=|\s+)([^\s]+)`)
	secretQueryPattern     = regexp.MustCompile(`(?i)([?&](?:token|access_token|auth_token|api_key|apikey|password|passwd|secret|signature|sig|x-amz-signature)=)[^&#\s]+`)
)

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
