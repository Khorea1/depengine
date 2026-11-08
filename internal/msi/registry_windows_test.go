//go:build windows

package msi

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	regCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	regSetValueExW  = advapi32.NewProc("RegSetValueExW")
	regDeleteTreeW  = advapi32.NewProc("RegDeleteTreeW")
)

func testRegistryKey(t *testing.T) syscall.Handle {
	t.Helper()
	path, err := syscall.UTF16PtrFromString(fmt.Sprintf(`Software\depengine-registry-test-%d`, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	var key syscall.Handle
	var disposition uint32
	status, _, _ := regCreateKeyExW.Call(
		uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(path)), 0, 0, 0,
		uintptr(syscall.KEY_READ|syscall.KEY_SET_VALUE), 0,
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition)),
	)
	if status != 0 {
		t.Fatalf("RegCreateKeyExW status = %d", status)
	}
	t.Cleanup(func() {
		_ = syscall.RegCloseKey(key)
		_, _, _ = regDeleteTreeW.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(path)))
	})
	return key
}

func setTestRegistryValue(t *testing.T, key syscall.Handle, name string, valueType uint32, data []byte) {
	t.Helper()
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	var dataPtr uintptr
	if len(data) != 0 {
		dataPtr = uintptr(unsafe.Pointer(&data[0]))
	}
	status, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(valueName)), 0, uintptr(valueType), dataPtr, uintptr(len(data)))
	if status != 0 {
		t.Fatalf("RegSetValueExW(%q) status = %d", name, status)
	}
}

func TestRegistryStringHandlesEmptyAndUnexpectedTypes(t *testing.T) {
	key := testRegistryKey(t)
	setTestRegistryValue(t, key, "Empty", syscall.REG_SZ, nil)
	setTestRegistryValue(t, key, "Unexpected", syscall.REG_DWORD, []byte{1, 0, 0, 0})

	got, err := registryString(key, "Empty")
	if err != nil || got != "" {
		t.Fatalf("empty REG_SZ = (%q, %v), want empty string without error", got, err)
	}
	if _, err := registryString(key, "Unexpected"); err == nil {
		t.Fatal("REG_DWORD accepted as a string")
	}
}
