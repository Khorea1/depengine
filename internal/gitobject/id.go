// Package gitobject contains validation helpers for Git object identities.
package gitobject

// ValidID reports whether value is a full lowercase SHA-1 or SHA-256 Git
// object ID. The hash length alone does not establish the object's Git type.
func ValidID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
