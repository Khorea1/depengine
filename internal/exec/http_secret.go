package exec

import "context"

type httpArtifactBearerKey struct{}

// WithHTTPArtifactBearer carries an ephemeral credential to the HTTP adapter
// for one real install call. The value stays in the execution context and is
// deliberately absent from plans, method configuration, and adapter argv.
func WithHTTPArtifactBearer(ctx context.Context, credential string) context.Context {
	return context.WithValue(ctx, httpArtifactBearerKey{}, credential)
}

// HTTPArtifactBearer reads the credential installed by the executor for the
// current HTTP artifact request. It is intended for the HTTP adapter only.
func HTTPArtifactBearer(ctx context.Context) (string, bool) {
	credential, ok := ctx.Value(httpArtifactBearerKey{}).(string)
	return credential, ok && credential != ""
}
