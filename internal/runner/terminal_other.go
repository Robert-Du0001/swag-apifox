//go:build !windows

package runner

// enableVirtualTerminal is a no-op off Windows: ANSI support is decided by
// shouldColor (TTY detection plus NO_COLOR / TERM / CLICOLOR_FORCE).
func enableVirtualTerminal() (func(), bool) {
	return func() {}, true
}
