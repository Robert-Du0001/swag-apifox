package transform

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
	"github.com/Robert-Du0001/swag-apifox/internal/labels"
)

// ApifoxEnum generates x-apifox-enum blocks from swag's enum metadata and
// from Chinese descriptions. It covers Step 0, Step 1 and Step 2.
type ApifoxEnum struct{ cfg Config }

func (t *ApifoxEnum) Name() string { return "enum" }

var reApifoxBlock = regexp.MustCompile(`^(\s*)x-apifox-enum:\s*$`)

// Strip removes previously generated x-apifox-enum blocks. It deliberately
// does NOT remove enum: blocks - on a second run those make hasEnum true and
// prevent a duplicate enum: from being inserted.
func (t *ApifoxEnum) Strip(doc *document.Document) {
	var clean []string
	lines := doc.Lines
	for i := 0; i < len(lines); i++ {
		if m := reApifoxBlock.FindStringSubmatch(lines[i]); m != nil {
			k := len(m[1])
			j := i + 1
			for j < len(lines) && (document.IsBlank(lines[j]) || document.Indent(lines[j]) > k) {
				j++
			}
			i = j - 1
			continue
		}
		clean = append(clean, lines[i])
	}
	doc.Lines = clean
}

func enumBlock(indent int, values []string) []string {
	out := []string{document.Spaces(indent) + "enum:"}
	for _, v := range values {
		out = append(out, document.Spaces(indent)+"- "+document.FormatValue(v))
	}
	return out
}

func apifoxBlock(indent int, items []Item, labelField string) []string {
	out := []string{document.Spaces(indent) + "x-apifox-enum:"}
	for _, it := range items {
		out = append(out, document.Spaces(indent+2)+"- value: "+document.FormatValue(it.Value))
		label := document.FormatLabel(it.Label)
		switch labelField {
		case "name":
			out = append(out,
				document.Spaces(indent+4)+"name: "+label,
				document.Spaces(indent+4)+"description: ''")
		case "both":
			out = append(out,
				document.Spaces(indent+4)+"name: "+label,
				document.Spaces(indent+4)+"description: "+label)
		default:
			out = append(out,
				document.Spaces(indent+4)+"name: ''",
				document.Spaces(indent+4)+"description: "+label)
		}
	}
	return out
}

var reQuotedComment = regexp.MustCompile(`^'(.*)'$`)

func (t *ApifoxEnum) Apply(ctx *Context) ([]document.Edit, error) {
	var edits []document.Edit
	lines := ctx.Doc.Lines

	// Step 1: schema-level enums (x-enum-comments + x-enum-varnames).
	for _, n := range ctx.Nodes {
		info := document.ReadNode(lines, n)
		comments, hasComments := info.Maps["x-enum-comments"]
		values, hasEnum := info.Lists["enum"]
		if !hasComments || !hasEnum {
			continue
		}
		varnames := info.Lists["x-enum-varnames"]
		descLabels := labels.Parse(document.DescriptionText(lines, n, info), t.cfg.StringEnums)
		items := make([]Item, 0, len(values))
		for i, v := range values {
			label := ""
			if i < len(varnames) {
				if c, ok := comments[varnames[i]]; ok {
					label = c
					if m := reQuotedComment.FindStringSubmatch(label); m != nil {
						label = strings.ReplaceAll(m[1], "''", "'")
					}
				}
			}
			if label == "" && descLabels.Contains(v) {
				label = descLabels.Get(v)
			}
			items = append(items, Item{Value: v, Label: label})
		}
		ctx.SchemaItems.Set(n.Key, items)
		insertAt := info.Props["enum"].Index + len(values) + 1
		edits = append(edits, document.Edit{
			Index: insertAt,
			Lines: apifoxBlock(n.Indent+2, items, t.cfg.EnumLabelField),
			Order: 0,
		})
		ctx.Stats.SchemaEnumCount++
	}

	// Step 2: field/parameter-level enums.
	for _, n := range ctx.Nodes {
		info := document.ReadNode(lines, n)
		if _, done := info.Maps["x-enum-comments"]; done {
			continue
		}
		values, hasEnum := info.Lists["enum"]
		parsed := labels.Parse(document.DescriptionText(lines, n, info), t.cfg.StringEnums)

		// query/form parameters keep their description on the parent list item
		if n.Key == "schema" {
			if pd := labels.ParentDescription(lines, n.Start); pd != "" {
				parsed.AddMissing(labels.Parse(pd, t.cfg.StringEnums))
			}
		}

		var refItems []Item
		if p, ok := info.Props["$ref"]; ok {
			ref := strings.Trim(strings.TrimSpace(p.Value), "'")
			name := ref[strings.LastIndex(ref, "/")+1:]
			refItems = ctx.SchemaItems.Get(name)
		}

		// Enums(...) parameters have values but no labels: borrow the labels
		// of the unique best-matching schema enum (at least 2 matches) so
		// short 1/2 style enums are never mislabelled.
		if hasEnum && refItems == nil {
			var need []string
			for _, v := range values {
				if !parsed.Contains(v) {
					need = append(need, v)
				}
			}
			if len(need) >= 2 {
				var best []Item
				bestScore, ties := 1, 0
				for _, k := range ctx.SchemaItems.Keys() {
					it := ctx.SchemaItems.Get(k)
					score := 0
					for _, v := range need {
						for _, ri := range it {
							if ri.Value == v && ri.Label != "" {
								score++
								break
							}
						}
					}
					if score > bestScore {
						best, bestScore, ties = it, score, 1
					} else if score == bestScore && score > 1 {
						ties++
					}
				}
				if best != nil && ties == 1 {
					refItems = best
				}
			}
		}

		if !hasEnum && parsed.Len() < 2 {
			continue
		}
		if hasEnum && parsed.Len() == 0 && refItems == nil {
			continue
		}

		var vals []string
		if hasEnum {
			vals = values
		} else {
			vals = parsed.Keys
		}
		items := make([]Item, 0, len(vals))
		for _, v := range vals {
			label := ""
			if parsed.Contains(v) {
				label = parsed.Get(v)
			}
			if label == "" {
				for _, ri := range refItems {
					if ri.Value == v {
						label = ri.Label
						break
					}
				}
			}
			items = append(items, Item{Value: v, Label: label})
		}

		var block []string
		if !hasEnum {
			block = append(block, enumBlock(n.Indent+2, vals)...)
		}
		block = append(block, apifoxBlock(n.Indent+2, items, t.cfg.EnumLabelField)...)
		edits = append(edits, document.Edit{Index: n.End + 1, Lines: block, Order: 1})
		ctx.Stats.FieldEnumCount++
	}
	return edits, nil
}

var (
	reValueItem = regexp.MustCompile(`^\s*-\s+value:`)
	reEmptyName = regexp.MustCompile(`^\s*name:\s*''\s*$`)
	reEmptyDesc = regexp.MustCompile(`^\s*description:\s*''\s*$`)
)

// Validate implements --strict: every x-apifox-enum block must have the same
// number of entries as the enum list in its node, and no entry may have both
// name and description empty.
func Validate(doc *document.Document) error {
	lines := doc.Lines
	for _, n := range document.IndexNodes(lines) {
		p := n.Indent + 2
		// Only the node that directly owns x-apifox-enum: is responsible for
		// it. Ancestors (e.g. components:) contain many such blocks deeper
		// down and must not count them.
		blockStart := -1
		for i := n.Start + 1; i <= n.End && i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "x-apifox-enum:" && document.Indent(lines[i]) == p {
				blockStart = i
				break
			}
		}
		if blockStart < 0 {
			continue
		}
		count, empty := 0, 0
		for i := blockStart + 1; i <= n.End && i < len(lines); i++ {
			line := lines[i]
			if !document.IsBlank(line) && document.Indent(line) <= p {
				break
			}
			if reValueItem.MatchString(line) && document.Indent(line) == p+2 {
				count++
				if i+2 < len(lines) && reEmptyName.MatchString(lines[i+1]) && reEmptyDesc.MatchString(lines[i+2]) {
					empty++
				}
			}
		}
		info := document.ReadNode(lines, n)
		if len(info.Lists["enum"]) != count {
			return fmt.Errorf("node %q at line %d: enum has %d values but x-apifox-enum has %d entries",
				n.Key, n.Start+1, len(info.Lists["enum"]), count)
		}
		if empty > 0 {
			return fmt.Errorf("node %q at line %d: %d enum entries have no label", n.Key, n.Start+1, empty)
		}
	}
	return nil
}
