package httpdownload

import (
	"compress/bzip2"
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
