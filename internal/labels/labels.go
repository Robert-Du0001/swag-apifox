// Package labels extracts "value -> Chinese label" pairs from the human
// readable descriptions that swag copies out of Go comments.
package labels

import (
	"regexp"
	"unicode/utf8"
)

// Ordered is an insertion-ordered string map: the first appearance of a
// value decides both the generated enum order and the tie-breaking in
// best-match. Go maps cannot preserve that, so keys are kept in a slice.
type Ordered struct {
	Keys []string
	M    map[string]string
}

func NewOrdered() *Ordered { return &Ordered{M: map[string]string{}} }

// Set inserts a key once (first writer wins).
func (o *Ordered) Set(k, v string) {
	if _, ok := o.M[k]; !ok {
		o.Keys = append(o.Keys, k)
		o.M[k] = v
	}
}

func (o *Ordered) Contains(k string) bool { _, ok := o.M[k]; return ok }
func (o *Ordered) Get(k string) string    { return o.M[k] }
func (o *Ordered) Len() int               { return len(o.Keys) }

// AddMissing copies pairs that are not present yet (used for parent
// descriptions and for the secondary regex).
func (o *Ordered) AddMissing(other *Ordered) {
	for _, k := range other.Keys {
		if !o.Contains(k) {
			o.Set(k, other.Get(k))
		}
	}
}

var (
	// "+ 日常收支：1" / "+ Name: 1" (name first, value last).
	// The original .NET regex ended with (?!\d); RE2 has no lookahead, but
	// [0-9]+ is greedy and nothing follows it, so the assertion was redundant.
	rePlus = regexp.MustCompile(`\+\s*([^\s:\x{FF1A}\x{3001}\x{FF0C},+]+)\s*[:\x{FF1A}]\s*([0-9]+)`)

	// "1-禁用, 2：启用" / "1 启用". The label must contain at least one Han
	// character, which is what stops dates (2026-11-06) and amounts from
	// being mistaken for enums.
	//
	// The trailing character class is only a *coarse* stop set: a regexp
	// cannot express "括号要配对", so it always looks one rune at a time and
	// has no memory of an opening bracket. What it produces is therefore just
	// the first chunk of the label; scanLabelEnd re-reads the text from the
	// label start and decides the real end (balanced brackets, see below).
	reAlt = regexp.MustCompile(`([0-9]+)\s*[-\x{2014}:\x{FF1A}]\s*([^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)|([0-9]+)\s+([\x{4e00}-\x{9fff}][^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)`)
	reHan = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)

	// Optional enhancement: string-valued enums such as "ASC-升序" or
	// "zm-众马". Off by default so the default output stays byte-identical.
	reStringEnum = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_.\-]*)\s*[-\x{2014}:\x{FF1A}]\s*([\p{Han}][^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)`)

	reParentDesc = regexp.MustCompile(`^\s*-\s+description:\s*(.*)$`)
)

// pair is one "value -> label" hit before the Han gate is applied.
type pair struct {
	value string
	label string
}

// scanLabelEnd returns the byte offset where a label that starts at start
// ends. The end is decided by a hand-written scan instead of the regexp tail
// because a label may contain a parenthetical aside, and parentheses must be
// balanced as a whole:
//
//   - separators (whitespace ， , 、 ； ; / 。) end the label only at bracket
//     depth 0 — "只读（0 = 不改；管理员永远是 1）" keeps the ；
//   - an opening bracket （ or ( raises the depth, so everything inside is
//     part of the label, including separators;
//   - a closing bracket ） or ) lowers it, and is only a terminator when it
//     has no opener inside the label (then it belongs to some outer text);
//   - an opening bracket still unclosed when the text runs out is dropped
//     together with the rest, so the label never carries a half bracket:
//     "启用（默认" -> "启用".
//
// This is why the previous implementation was wrong: the regexp stopped at ）
// but happily consumed （, producing "读写（默认" — one bracket, no pair.
func scanLabelEnd(text string, start int) int {
	depth := 0
	// Offset of the outermost still-open bracket; -1 when everything is closed.
	unclosed := -1

	for i := start; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == '（' || r == '(':
			if depth == 0 {
				unclosed = i
			}
			depth++
		case r == '）' || r == ')':
			if depth == 0 {
				return i
			}
			depth--
			if depth == 0 {
				unclosed = -1
			}
		case isLabelSeparator(r) && depth == 0:
			return i
		}

		i += size
	}

	if depth > 0 {
		return unclosed
	}

	return len(text)
}

// isLabelSeparator reports whether r ends a label when it appears outside
// brackets. The set matches the negated classes in reAlt / reStringEnum, plus
// the CJK full stop so "只读（管理员恒为 1）。" does not keep the trailing 。
func isLabelSeparator(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', '\u00A0', '\u3000',
		',', '，', '、', ';', '；', '/', '。':
		return true
	default:
		return false
	}
}

// altPairs extracts the "1-禁用" / "1 启用" forms. reAlt has two branches, so
// the value and the label start come from different submatch indexes.
func altPairs(text string) []pair {
	matches := reAlt.FindAllStringSubmatchIndex(text, -1)
	out := make([]pair, 0, len(matches))
	for _, m := range matches {
		var value string
		var labelStart int
		if m[6] >= 0 { // second branch: "1 启用"
			value, labelStart = text[m[6]:m[7]], m[8]
		} else { // first branch: "1-禁用"
			value, labelStart = text[m[2]:m[3]], m[4]
		}

		out = append(out, pair{value: value, label: text[labelStart:scanLabelEnd(text, labelStart)]})
	}

	return out
}

// stringEnumPairs extracts the "ASC-升序" form.
func stringEnumPairs(text string) []pair {
	matches := reStringEnum.FindAllStringSubmatchIndex(text, -1)
	out := make([]pair, 0, len(matches))
	for _, m := range matches {
		out = append(out, pair{value: text[m[2]:m[3]], label: text[m[4]:scanLabelEnd(text, m[4])]})
	}

	return out
}

// Parse reproduces Get-EnumLabels. Two passes:
//  1. "+ name: value" pairs (label does not have to be Chinese).
//  2. Only when fewer than 2 pairs were found, "value-label" pairs whose
//     label contains a Han character.
//
// Pass 1 and 2 differ in how the label is delimited: rePlus only ever reads up
// to the colon (no brackets involved), while pass 2 rebuilds the label with
// scanLabelEnd so a parenthetical aside is kept or dropped as a whole.
func Parse(text string, stringEnums bool) *Ordered {
	result := NewOrdered()
	for _, m := range rePlus.FindAllStringSubmatch(text, -1) {
		result.Set(m[2], m[1])
	}
	if result.Len() < 2 {
		alt := NewOrdered()
		for _, p := range altPairs(text) {
			if !reHan.MatchString(p.label) {
				continue
			}
			alt.Set(p.value, p.label)
		}
		result.AddMissing(alt)
	}
	if stringEnums {
		extra := NewOrdered()
		for _, p := range stringEnumPairs(text) {
			extra.Set(p.value, p.label)
		}
		result.AddMissing(extra)
	}
	return result
}

// ParentDescription walks back at most 8 lines from a schema node to find the
// "- description:" line of a query/form parameter. swag puts the parameter
// description on the parent list item while enum lives inside schema.
func ParentDescription(lines []string, start int) string {
	stop := start - 8
	if stop < 0 {
		stop = 0
	}
	for u := start - 1; u >= stop && u >= 0 && u < len(lines); u-- {
		if m := reParentDesc.FindStringSubmatch(lines[u]); m != nil {
			return m[1]
		}
	}
	return ""
}
