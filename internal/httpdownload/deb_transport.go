package httpdownload

// NewDebTransportHTTPAdapter is a transport-only adapter for the dedicated
// dpkg lifecycle. It preserves normal HTTP's .deb rejection while allowing the
// deb adapter to download and verify a staged package before dpkg owns it.
func NewDebTransportHTTPAdapter() *HTTPAdapter {
	return &HTTPAdapter{allowInstallerArtifacts: true, allowDebArtifacts: true}
}
