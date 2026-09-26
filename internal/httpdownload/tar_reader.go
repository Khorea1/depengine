package httpdownload

import (
	"compress/bzip2"
	"context"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// openStdlibTarReader opens a tar archive in one of the compression formats
// supported by the Go standard library. The caller owns the returned reader
// and must close it. Decompression is intentionally separated from tar entry
// materialization so every supported format feeds the same rooted archive
// writer.
func externalTarDecoder(src, ext string) (string, []string, error) {
	switch ext {
	case ".tar.xz":
		return "xz", []string{"-d", "-c", "--", src}, nil
	case ".tar.zst":
		return "zstd", []string{"-d", "-c", "--", src}, nil
	default:
		return "", nil, fmt.Errorf("tar archive compression %q has no external decoder", ext)
	}
}

func openStdlibTarReader(src, ext string) (io.ReadCloser, error) {
	f, err := os.Open(src) // #nosec G304 -- src is the depengine-managed downloaded archive path.
	if err != nil {
		return nil, fmt.Errorf("open tar archive %q: %w", src, err)
	}

	switch ext {
	case ".tar":
		return f, nil
	case ".tar.gz", ".tgz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("open gzip tar archive %q: %w", src, err)
		}
		return &compoundReadCloser{
			Reader:  gz,
			closers: []io.Closer{gz, f},
		}, nil
	case ".tar.bz2":
		return &compoundReadCloser{
			Reader:  bzip2.NewReader(f),
			closers: []io.Closer{f},
		}, nil
	default:
		_ = f.Close()
		return nil, fmt.Errorf("tar archive compression %q is not supported by the Go standard library", ext)
	}
}


// maxTarTrailingBytes bounds bytes following the TAR end markers while still
// allowing ordinary record padding produced by system tar implementations.
// Draining this tail forces gzip/bzip2 readers to validate their stream trailer
// without permitting an unbounded decompression sink after the logical archive.
const maxTarTrailingBytes int64 = 16 << 20

func verifyCompressedTarTrailer(ctx context.Context, r io.Reader) error {
	limited := &io.LimitedReader{
		R: contextReader{ctx: ctx, r: r},
		N: maxTarTrailingBytes + 1,
	}
	n, err := io.Copy(io.Discard, limited)
	if err != nil {
		return err
	}
	if n > maxTarTrailingBytes {
		return fmt.Errorf("tar trailing data exceeds %d-byte limit", maxTarTrailingBytes)
	}
	return nil
}

type compoundReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (r *compoundReadCloser) Close() error {
	var errs []error
	for _, closer := range r.closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
