package platform

// normalizeRuntimeArch maps Go's architecture vocabulary to the uname-style
// spellings historically exposed by the full host detector. The detector
// still prefers uname -m when subprocess execution is available; this mapping
// is the deterministic fallback for hosts where that probe cannot run.
func normalizeRuntimeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i386"
	default:
		if goarch == "" {
			return "unknown"
		}
		return goarch
	}
}
