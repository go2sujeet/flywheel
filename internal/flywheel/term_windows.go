package flywheel

import (
	"os"
	"syscall"
	"unsafe"
)

// EnableANSI turns on Windows virtual-terminal processing for stdout so ANSI
// colour and cursor escapes render, and reports whether ANSI sequences will be
// interpreted. It is false when stdout is not a character device (a pipe or a
// redirected file), so piped output never carries escape sequences.
func EnableANSI() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	if (info.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	k32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := k32.NewProc("GetConsoleMode")
	setMode := k32.NewProc("SetConsoleMode")
	h := uintptr(syscall.Handle(os.Stdout.Fd()))
	var mode uintptr
	if _, _, err := getMode.Call(h, uintptr(unsafe.Pointer(&mode))); err != nil {
		return false
	}
	const vt = 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if _, _, err := setMode.Call(h, mode|vt); err != nil {
		return false
	}
	return true
}
