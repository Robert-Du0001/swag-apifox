package runner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
)

// ANSI escapes used for the swag log stream.
const (
	ansiReset  = "\x1b[0m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
)

// Supported --color values.
const (
	ColorAuto   = "auto"
	ColorAlways = "always"
	ColorNever  = "never"
)

// shouldColor decides whether ANSI escapes may be written to stderr.
// "auto" honours NO_COLOR / CLICOLOR_FORCE / TERM=dumb and only enables
// colour when stderr is an interactive terminal.
func shouldColor(mode string) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		if os.Getenv("NO_COLOR") != "" {
			return false
		}
		if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
			return true
		}
		if os.Getenv("TERM") == "dumb" {
			return false
		}
		fi, err := os.Stderr.Stat()
		if err != nil {
			return false
		}
		// A regular file means stderr was redirected; never write escapes there.
		if fi.Mode().IsRegular() {
			return false
		}
		if fi.Mode()&os.ModeCharDevice != 0 {
			return true
		}
		// On Windows a pipe is normally an ANSI-capable pty (mintty, Windows
		// Terminal, ConPTY); enableVirtualTerminal handles the classic console.
		return runtime.GOOS == "windows"
	}
}

// prepareColor resolves the requested mode and, on Windows, enables VT
// processing when needed. The returned restore function must be deferred.
func prepareColor(mode string) (bool, func()) {
	if mode == "" {
		mode = ColorAuto
	}
	color := shouldColor(mode)
	restore := func() {}
	if color {
		var ok bool
		restore, ok = enableVirtualTerminal()
		if !ok && mode != ColorAlways {
			color = false
		}
	}
	return color, restore
}

// ansiTerminal reports terminals that interpret escape sequences themselves,
// so they neither need nor can use Windows' SetConsoleMode.
func ansiTerminal() bool {
	if os.Getenv("WT_SESSION") != "" || os.Getenv("ConEmuANSI") == "ON" || os.Getenv("ANSICON") != "" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	for _, hint := range []string{"xterm", "cygwin", "mintty", "screen", "tmux", "vt100", "ansi"} {
		if term != "" && strings.Contains(term, hint) {
			return true
		}
	}
	return false
}

// classify picks a colour for one swag log line. Progress is cyan, known
// non-fatal notices are yellow and anything that looks like a failure is red.
// An empty result means "leave the line uncoloured".
func classify(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error"),
		strings.Contains(lower, "cannot find"),
		strings.Contains(lower, "failed"),
		strings.Contains(lower, "panic:"):
		return ansiRed
	case strings.Contains(lower, "warn"),
		strings.Contains(lower, "override detected"),
		strings.Contains(lower, "found schema with no type"),
		strings.Contains(lower, "deprecated"):
		return ansiYellow
	case strings.Contains(lower, "generating"),
		strings.Contains(lower, "generate "),
		strings.Contains(lower, "create "):
		return ansiCyan
	default:
		return ""
	}
}

// swagLog is a line-buffered io.Writer that forwards swag's output as it
// arrives (so the command never looks frozen) and colourises whole lines.
// exec.Cmd may write from two goroutines when Stdout and Stderr share a
// writer, hence the mutex.
type swagLog struct {
	w     io.Writer
	color bool
	mu    sync.Mutex
	buf   []byte
}

func newSwagLog(w io.Writer, color bool) *swagLog {
	return &swagLog{w: w, color: color}
}

func (l *swagLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.emit(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	// Don't buffer forever if swag emits one huge line without a newline.
	if len(l.buf) >= 64<<10 {
		l.emit(l.buf)
		l.buf = l.buf[:0]
	}
	return len(p), nil
}

// Flush emits a trailing fragment that did not end with a newline.
func (l *swagLog) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.emit(l.buf)
		l.buf = l.buf[:0]
	}
}

func (l *swagLog) emit(line []byte) {
	s := strings.TrimSuffix(string(line), "\r")
	if s == "" {
		fmt.Fprintln(l.w)
		return
	}
	if code := classify(s); l.color && code != "" {
		fmt.Fprintf(l.w, "%s%s%s\n", code, s, ansiReset)
		return
	}
	fmt.Fprintln(l.w, s)
}
