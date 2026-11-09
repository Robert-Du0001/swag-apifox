// Package labels extracts "value -> Chinese label" pairs from the human
// readable descriptions that swag copies out of Go comments.
package labels

import "regexp"

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
	reAlt = regexp.MustCompile(`([0-9]+)\s*[-\x{2014}:\x{FF1A}]\s*([^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)|([0-9]+)\s+([\x{4e00}-\x{9fff}][^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)`)
	reHan = regexp.MustCompile(`[\x{4e00}-\x{9fff}]`)

	// Optional enhancement: string-valued enums such as "ASC-升序" or
	// "zm-众马". Off by default so the default output stays byte-identical.
	reStringEnum = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_.\-]*)\s*[-\x{2014}:\x{FF1A}]\s*([\p{Han}][^\s\x{FF0C},\x{3001}\x{FF1B};/\x{FF09})]*)`)

	reParentDesc = regexp.MustCompile(`^\s*-\s+description:\s*(.*)$`)
)

// Parse reproduces Get-EnumLabels. Two passes:
//  1. "+ name: value" pairs (label does not have to be Chinese).
//  2. Only when fewer than 2 pairs were found, "value-label" pairs whose
//     label contains a Han character.
func Parse(text string, stringEnums bool) *Ordered {
	result := NewOrdered()
	for _, m := range rePlus.FindAllStringSubmatch(text, -1) {
		result.Set(m[2], m[1])
	}
	if result.Len() < 2 {
		alt := NewOrdered()
		for _, m := range reAlt.FindAllStringSubmatch(text, -1) {
			v, lab := m[1], m[2]
			if m[3] != "" {
				v, lab = m[3], m[4]
			}
			if !reHan.MatchString(lab) {
				continue
			}
			alt.Set(v, lab)
		}
		result.AddMissing(alt)
	}
	if stringEnums {
		extra := NewOrdered()
		for _, m := range reStringEnum.FindAllStringSubmatch(text, -1) {
			extra.Set(m[1], m[2])
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
