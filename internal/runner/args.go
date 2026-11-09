package runner

import (
	"fmt"
	"strings"
	"unicode"
)

// splitArgs splits a command-line-style string into arguments. Single and
// double quotes group words and a backslash escapes the next character.
// No shell expansion is performed: this only exists so a whole argument list
// can be carried in one quoted argument after "--".
func splitArgs(s string) ([]string, error) {
	rs := []rune(s)
	var (
		args    []string
		buf     strings.Builder
		quote   rune
		started bool
	)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == '\\':
			if i+1 < len(rs) {
				i++
				buf.WriteRune(rs[i])
			} else {
				buf.WriteRune(r)
			}
			started = true
		case unicode.IsSpace(r):
			if started {
				args = append(args, buf.String())
				buf.Reset()
				started = false
			}
		default:
			buf.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in swag arguments: %q", s)
	}
	if started {
		args = append(args, buf.String())
	}
	return args, nil
}

// PassthroughArgs normalises the arguments written after "--". Two spellings
// are both supported:
//
//	build -- --parseDependency --parseDepth 1
//	build -- "--parseDependency --parseDepth 1"
//
// A single dash-prefixed argument containing whitespace is treated as a whole
// argument list and split with shell-style quoting. Everything else is passed
// through verbatim, so values that legitimately contain spaces (for example
// `--exclude "my dir"`) keep their shape.
func PassthroughArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") && strings.IndexFunc(a, unicode.IsSpace) >= 0 {
			parts, err := splitArgs(a)
			if err != nil {
				return nil, err
			}
			out = append(out, parts...)
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// swagCommandArgs builds the full argv for swag. The base command is fixed
// because the post-processor only understands OpenAPI 3.1 YAML. Everything
// else comes from the arguments after "--"; -o is always owned by the tool
// since the raw output has to land in the temp dir we read from.
func swagCommandArgs(cfg Config, outDir string) []string {
	args := []string{"init", "--v3.1", "--ot", "yaml"}
	args = append(args, cfg.SwagArgs...)
	args = append(args, "-o", outDir)
	return args
}
