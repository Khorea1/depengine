//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package httpdownload

import "testing"

func runAsUnprivilegedTest(*testing.T) bool { return false }
