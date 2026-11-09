package transform

import (
	"regexp"
	"strings"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
)

// Nullable converts x-nullable: true (produced by a Go struct tag
// extensions:"x-nullable") into type: 'null', keeping only descriptive keys.
type Nullable struct{}

func (t *Nullable) Name() string { return "nullable" }

// Strip is a no-op: the replacement removes x-nullable itself, so running
// twice cannot match again. A fully processed document is already stable.
func (t *Nullable) Strip(doc *document.Document) {}

var (
	// Match x-nullable case-insensitively; an empty value is also accepted.
	reXNullable = regexp.MustCompile(`(?i)^\s*x-nullable:\s*(true|1)?\s*$`)
	// NOTE: unlike ReadNode's property regex, this one does not allow '$',
	// so a sibling $ref is intentionally dropped.
	reKeepProp = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_\-]*):`)
)

func (t *Nullable) Apply(ctx *Context) ([]document.Edit, error) {
	keepKeys := map[string]bool{
		"description": true,
		"example":     true,
		"title":       true,
		"form":        true,
	}
	lines := ctx.Doc.Lines
	var edits []document.Edit
	for _, n := range ctx.Nodes {
		xn := -1
		for i := n.Start + 1; i <= n.End && i < len(lines); i++ {
			if reXNullable.MatchString(lines[i]) {
				xn = i
				break
			}
		}
		if xn < 0 || document.Indent(lines[xn]) != n.Indent+2 {
			continue
		}
		p := n.Indent + 2
		block := []string{lines[n.Start]}
		curKeep := false
		for i := n.Start + 1; i <= n.End && i < len(lines); i++ {
			line := lines[i]
			if document.IsBlank(line) {
				continue
			}
			if document.Indent(line) == p {
				m := reKeepProp.FindStringSubmatch(strings.TrimSpace(line))
				curKeep = m != nil && keepKeys[m[1]]
				if curKeep {
					block = append(block, line)
				}
				continue
			}
			if curKeep {
				block = append(block, line)
			}
		}
		block = append(block, document.Spaces(p)+"type: 'null'")
		edits = append(edits, document.Edit{
			Index:  n.Start,
			Remove: n.End - n.Start + 1,
			Lines:  block,
			Order:  2,
		})
		ctx.Stats.NullCount++
	}
	return edits, nil
}
