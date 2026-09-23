//go:build windows

package msi

import (
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
const errorNoMoreItems syscall.Errno = 259

type windowsRegistry struct{}

func newProductFinder() productFinder { return windowsRegistry{} }

func (windowsRegistry) Find(name, publisher string) (string, bool, error) {
	for _, root := range []syscall.Handle{syscall.HKEY_CURRENT_USER, syscall.HKEY_LOCAL_MACHINE} {
		for _, view := range []uint32{syscall.KEY_WOW64_64KEY, syscall.KEY_WOW64_32KEY} {
			code, ok, err := findInRegistry(root, view, name, publisher)
			if err != nil && err != syscall.ERROR_FILE_NOT_FOUND {
				return "", false, err
			}
			if ok {
				return code, true, nil
			}
		}
	}
	return "", false, nil
}

func findInRegistry(root syscall.Handle, view uint32, name, publisher string) (string, bool, error) {
	path, _ := syscall.UTF16PtrFromString(uninstallKey)
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(root, path, 0, syscall.KEY_READ|view, &key); err != nil {
		return "", false, err
	}
	defer syscall.RegCloseKey(key)
	for index := uint32(0); ; index++ {
		nameBuf := make([]uint16, 256)
		length := uint32(len(nameBuf))
		err := syscall.RegEnumKeyEx(key, index, &nameBuf[0], &length, nil, nil, nil, nil)
		if err == errorNoMoreItems {
			break
		}
		if err != nil {
			return "", false, err
		}
		subName := syscall.UTF16ToString(nameBuf[:length])
		if !strings.HasPrefix(subName, "{") || !strings.HasSuffix(subName, "}") {
			continue
		}
		subPtr, _ := syscall.UTF16PtrFromString(subName)
		var sub syscall.Handle
		if err := syscall.RegOpenKeyEx(key, subPtr, 0, syscall.KEY_READ|view, &sub); err != nil {
			continue
		}
		display, displayErr := registryString(sub, "DisplayName")
		vendor, vendorErr := registryString(sub, "Publisher")
		code := subName
		if productCode, err := registryString(sub, "ProductCode"); err == nil && productCode != "" {
			code = productCode
		}
		_ = syscall.RegCloseKey(sub) // best-effort; leaked handle is closed with the process
		if displayErr == nil && display == name && strings.HasPrefix(code, "{") && strings.HasSuffix(code, "}") && (publisher == "" || (vendorErr == nil && vendor == publisher)) {
			return code, true, nil
		}
	}
	return "", false, nil
}

func registryString(key syscall.Handle, name string) (string, error) {
	valueName, _ := syscall.UTF16PtrFromString(name)
	var valueType, size uint32
	if err := syscall.RegQueryValueEx(key, valueName, nil, &valueType, nil, &size); err != nil {
		return "", err
	}
	buf := make([]byte, size)
	if err := syscall.RegQueryValueEx(key, valueName, nil, &valueType, &buf[0], &size); err != nil {
		return "", err
	}
	words := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), len(buf)/2)
	for len(words) > 0 && words[len(words)-1] == 0 {
		words = words[:len(words)-1]
	}
	return string(utf16.Decode(words)), nil
}
