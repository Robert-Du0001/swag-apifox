//go:build windows

package runner

import "syscall"

const vtFlag = 0x0004 // ENABLE_VIRTUAL_TERMINAL_PROCESSING

// enableVirtualTerminal turns on ANSI escape processing for the console
// attached to stderr. Terminals such as mintty / Windows Terminal already
// interpret escapes, so they are left alone. The returned function restores
// the original console mode.
func enableVirtualTerminal() (func(), bool) {
	if ansiTerminal() {
		return func() {}, true
	}
	var mode uint32
	if err := syscall.GetConsoleMode(syscall.Stderr, &mode); err != nil {
		// Not a Win32 console (for example a pty pipe). There is no console
		// mode to change: the terminal on the other end interprets escapes.
		return func() {}, true
	}
	if mode&vtFlag != 0 {
		return func() {}, true
	}
	setMode := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	if r, _, _ := setMode.Call(uintptr(syscall.Stderr), uintptr(mode|vtFlag)); r == 0 {
		return func() {}, false
	}
	return func() {
		setMode.Call(uintptr(syscall.Stderr), uintptr(mode))
	}, true
}
