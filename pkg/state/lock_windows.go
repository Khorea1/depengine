//go:build windows

package state

import (
	"fmt"
	"io"
)

// lock is not supported on Windows.
func lock() (io.Closer, error) {
	return nil, fmt.Errorf("file locking is not supported on Windows")
}

// lockShared is not supported on Windows.
func lockShared() (io.Closer, error) {
	return nil, fmt.Errorf("file locking is not supported on Windows")
}
