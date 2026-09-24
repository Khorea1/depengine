//go:build !windows

package term

import (
	"os"
	"syscall"
	"unsafe"
)

// Width returns the width of the terminal attached to f in columns. It
// returns 0 when f is not a terminal or the width cannot be determined, so
// callers can tell "measured" apart from "assume a default".
func Width(f *os.File) int {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return 0
	}
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	ws := &winsize{}
	// TIOCGWINSZ: 0x5413 on Linux, 0x40087468 on the BSDs and macOS.
	for _, req := range []uintptr{0x5413, 0x40087468} {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(ws))) // #nosec G103 -- the ioctl ABI takes a raw pointer; the buffer is process-local and never dereferenced by third parties.
		if errno == 0 && ws.Col > 0 {
			return int(ws.Col)
		}
	}
	return 0
}
