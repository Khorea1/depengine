//go:build !windows

package exec

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalWidth returns the width of the terminal connected to stderr.
// Returns 0 if stderr is not a terminal or if the width cannot be determined.
func terminalWidth() int {
	fi, _ := os.Stderr.Stat()
	if fi == nil || fi.Mode()&os.ModeCharDevice == 0 {
		return 0
	}
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	ws := &winsize{}
	for _, req := range []uintptr{0x5413, 0x40087468} {
		_, _, err := syscall.Syscall(syscall.SYS_IOCTL, os.Stderr.Fd(), req, uintptr(unsafe.Pointer(ws)))
		if err == 0 && ws.Col > 0 {
			return int(ws.Col)
		}
	}
	return 0
}
