package exec

import "context"

// HTTPBearerPurpose identifies an authenticated request during an HTTP install.
type HTTPBearerPurpose string

const (
	HTTPBearerArtifact  HTTPBearerPurpose = "artifact"
	HTTPBearerChecksum  HTTPBearerPurpose = "checksum"
	HTTPBearerSignature HTTPBearerPurpose = "signature"
)

type httpBearerKey struct{ purpose HTTPBearerPurpose }

// WithHTTPBearer carries a request-specific credential in the install context.
func WithHTTPBearer(ctx context.Context, purpose HTTPBearerPurpose, credential string) context.Context {
	return context.WithValue(ctx, httpBearerKey{purpose}, credential)
}

// HTTPBearer reads the credential assigned to one request purpose.
func HTTPBearer(ctx context.Context, purpose HTTPBearerPurpose) (string, bool) {
	credential, ok := ctx.Value(httpBearerKey{purpose}).(string)
	return credential, ok && credential != ""
}

// WithHTTPArtifactBearer carries an ephemeral credential to the HTTP adapter
// for one real install call. The value stays in the execution context and is
// deliberately absent from plans, method configuration, and adapter argv.
func WithHTTPArtifactBearer(ctx context.Context, credential string) context.Context {
	return WithHTTPBearer(ctx, HTTPBearerArtifact, credential)
}

// HTTPArtifactBearer reads the credential installed by the executor for the
// current HTTP artifact request. It is intended for the HTTP adapter only.
func HTTPArtifactBearer(ctx context.Context) (string, bool) {
	return HTTPBearer(ctx, HTTPBearerArtifact)
}
