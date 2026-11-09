// Package document models a swag-generated OpenAPI YAML as plain text lines.
//
// Nothing in this package parses YAML. swag emits a very specific layout
// (2-space indentation, list items at the same indent as their key, keys in
// alphabetical order); re-serializing it would change indentation, key order
// and quoting. All transformations are therefore line edits on []string.
package document

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Document is the in-memory representation of the source file. NL is the
// newline sequence detected in the input ("\n" or "\r\n") and is reused on
// output so the file stays byte-identical where nothing changed.
type Document struct {
	Lines []string
	NL    string
}

var reSplit = regexp.MustCompile(`\r?\n`)

// ReadFile loads a document, transparently skipping a UTF-8 BOM.
func ReadFile(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(b), nil
}

// ParseBytes builds a Document from raw bytes. It detects CRLF anywhere in
// the text, then splits on /\r?\n/ keeping the trailing empty element
// produced by a final newline.
func ParseBytes(b []byte) *Document {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	text := string(b)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	return &Document{Lines: reSplit.Split(text, -1), NL: nl}
}

// Text joins the lines with the original newline sequence.
func (d *Document) Text() string { return strings.Join(d.Lines, d.NL) }

// WriteFile writes UTF-8 without BOM, creating parent directories as needed.
func (d *Document) WriteFile(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(d.Text()), 0o644)
}

// Indent returns the number of leading spaces. swag never emits tabs.
func Indent(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// Spaces returns n spaces.
func Spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// IsBlank reports whether a line contains only whitespace.
func IsBlank(line string) bool { return strings.TrimSpace(line) == "" }
