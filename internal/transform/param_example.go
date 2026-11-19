package transform

import (
	"regexp"
	"strings"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
)

// ParamExample turns the spec extension x-example into a real example.
//
// swag v2's defineType() rejects object and array types, so
// `example(a,b)` on a `[]string` query parameter is silently dropped. The
// extension attribute survives that filter: `extensions(x-example=[a,b])`
// ends up next to the parameter's schema. This transformer renames it to
// example: and expands the bracket form into a YAML list, so an array example
// can still reach the generated document without hand editing.
//
// Convention:
//
//	x-example: foo          -> example: foo
//	x-example: '1,2'        -> example: '1,2'
//	x-example: '[a,b]'      -> example:\n- a\n- b
//
// The brackets are required for the list form: they also stop swag's
// splitNotWrapped() from splitting the value on its commas.
type ParamExample struct{}

func (t *ParamExample) Name() string { return "example" }

// Strip is a no-op: the output no longer contains x-example, so a second run
// finds nothing to convert.
func (t *ParamExample) Strip(doc *document.Document) {}

var reXExample = regexp.MustCompile(`^(\s*)x-example:\s*(.*)$`)

func (t *ParamExample) Apply(ctx *Context) ([]document.Edit, error) {
	lines := ctx.Doc.Lines
	var edits []document.Edit
	for i, line := range lines {
		m := reXExample.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		raw := unquoteScalar(m[2])
		// Never emit two example keys in one mapping: swag only drops
		// example() for arrays, but a hand-written example: must win.
		if hasSiblingKey(lines, i, indent, "example") {
			edits = append(edits, document.Edit{Index: i, Remove: 1, Order: 3})
			continue
		}
		edits = append(edits, document.Edit{
			Index:  i,
			Remove: 1,
			Lines:  exampleBlock(indent, raw),
			Order:  3,
		})
	}
	return edits, nil
}

// unquoteScalar removes one layer of YAML quotes around a scalar.
func unquoteScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			inner := s[1 : len(s)-1]
			if s[0] == '\'' {
				inner = strings.ReplaceAll(inner, "''", "'")
			}
			return inner
		}
	}
	return s
}

// exampleBlock renders example: at the given indent. The bracket form becomes
// a YAML list whose items keep their scalar type (numbers stay numbers).
func exampleBlock(indent int, raw string) []string {
	pad := document.Spaces(indent)
	if !(strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]")) {
		return []string{pad + "example: " + document.FormatValue(raw)}
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return []string{pad + "example: []"}
	}
	out := []string{pad + "example:"}
	for _, item := range strings.Split(inner, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, pad+"- "+document.FormatValue(unquoteScalar(item)))
	}
	if len(out) == 1 {
		return []string{pad + "example: []"}
	}
	return out
}

// hasSiblingKey reports whether the mapping that owns line at indent already
// has key, scanning upwards and downwards within the same indent level.
func hasSiblingKey(lines []string, at, indent int, key string) bool {
	want := key + ":"
	sameLevel := func(i int) (bool, bool) {
		line := lines[i]
		if document.IsBlank(line) {
			return false, true
		}
		ind := document.Indent(line)
		if ind < indent {
			return false, false
		}
		if ind > indent {
			return false, true
		}
		return strings.HasPrefix(strings.TrimSpace(line), want), true
	}
	for i := at - 1; i >= 0; i-- {
		match, cont := sameLevel(i)
		if match {
			return true
		}
		if !cont {
			break
		}
	}
	for i := at + 1; i < len(lines); i++ {
		match, cont := sameLevel(i)
		if match {
			return true
		}
		if !cont {
			break
		}
	}
	return false
}
