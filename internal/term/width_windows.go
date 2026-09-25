//go:build windows

package term

import (
	"os"
	"syscall"
	"unsafe"
)

type consoleCoord struct {
	X int16
	Y int16
}

type consoleSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type consoleScreenBufferInfo struct {
	Size              consoleCoord
	CursorPosition    consoleCoord
	Attributes        uint16
	Window            consoleSmallRect
	MaximumWindowSize consoleCoord
}

var getConsoleScreenBufferInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")

// Width returns the visible console window width attached to f in columns.
// Redirected files/pipes and handles that are not console screen buffers return
// 0 so callers can fall back to another stream or the default width.
func Width(f *os.File) int {
	if f == nil {
		return 0
	}
	var info consoleScreenBufferInfo
	ok, _, _ := getConsoleScreenBufferInfo.Call(
		f.Fd(),
		uintptr(unsafe.Pointer(&info)), // #nosec G103 -- Win32 fills this process-local output structure synchronously.
	)
	if ok == 0 {
		return 0
	}
	width := int(info.Window.Right-info.Window.Left) + 1
	if width <= 0 {
		return 0
	}
	return width
}
