package httpdownload

import (
	"context"

	"github.com/Khorea1/depengine/internal/integrity"
	"github.com/Khorea1/depengine/internal/run"
)

// GPGVerify preserves the HTTP adapter's historical API while delegating all
// signature semantics to the adapter-neutral integrity package. HTTP key
// references keep using GoDownloader so existing authentication/redirect
// behavior remains unchanged.
func GPGVerify(ctx context.Context, rn run.Runner, checksumFile, signatureFile, signingKey string) error {
	dl := NewGoDownloader(rn)
	return integrity.GPGVerify(ctx, rn, checksumFile, signatureFile, signingKey, dl.Download)
}
