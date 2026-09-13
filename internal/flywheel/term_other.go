//go:build !windows

package flywheel

// EnableANSI reports whether stdout is a character device that will interpret
// ANSI sequences. On non-Windows platforms an ANSI terminal needs no explicit
// setup: if stdout is a character device, colour and cursor escapes render.
func EnableANSI() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
