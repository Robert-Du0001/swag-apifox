package document

import (
	"regexp"
	"strings"
)

// Node is a line range whose declaration line is only "key:" (no scalar
// value). End is inclusive and includes trailing blank lines.
type Node struct {
	Start  int
	End    int
	Indent int
	Key    string
}

var reNode = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.\-]+):\s*$`)

// IndexNodes finds every "key:" node and its inclusive line range.
func IndexNodes(lines []string) []Node {
	var nodes []Node
	for i, line := range lines {
		m := reNode.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		k := len(m[1])
		j := i + 1
		for j < len(lines) {
			if !IsBlank(lines[j]) && Indent(lines[j]) <= k {
				break
			}
			j++
		}
		nodes = append(nodes, Node{Start: i, End: j - 1, Indent: k, Key: m[2]})
	}
	return nodes
}

// Prop is a direct scalar property of a node ("type: string").
type Prop struct {
	Index int
	Value string
}

// NodeInfo is the direct-child view of one node:
//   - Props: scalar properties (including $ref)
//   - Lists: list items, e.g. enum: followed by "- 1" at the SAME indent
//   - Maps: nested maps, e.g. x-enum-comments: { ConstName: 中文 }
type NodeInfo struct {
	Props map[string]Prop
	Lists map[string][]string
	Maps  map[string]map[string]string
}

var (
	reListItem = regexp.MustCompile(`^-\s+(.*)$`)
	// Direct property names may start with '$' so that $ref is recognised.
	reProp = regexp.MustCompile(`^([A-Za-z_$][A-Za-z0-9_\-]*):\s*(.*)$`)
	reSub  = regexp.MustCompile(`^([^:]+):\s*(.*)$`)
)

// ReadNode interprets the direct children of n at indent n.Indent+2.
func ReadNode(lines []string, n Node) *NodeInfo {
	p := n.Indent + 2
	info := &NodeInfo{
		Props: map[string]Prop{},
		Lists: map[string][]string{},
		Maps:  map[string]map[string]string{},
	}
	current := ""
	for i := n.Start + 1; i <= n.End && i < len(lines); i++ {
		line := lines[i]
		if IsBlank(line) {
			continue
		}
		ind := Indent(line)
		trim := strings.TrimSpace(line)
		if ind == p {
			if m := reListItem.FindStringSubmatch(trim); m != nil && current != "" {
				info.Lists[current] = append(info.Lists[current], m[1])
				continue
			}
			if m := reProp.FindStringSubmatch(trim); m != nil {
				current = m[1]
				info.Props[current] = Prop{Index: i, Value: m[2]}
				continue
			}
		}
		if ind > p && current != "" {
			if m := reSub.FindStringSubmatch(trim); m != nil {
				if info.Maps[current] == nil {
					info.Maps[current] = map[string]string{}
				}
				info.Maps[current][m[1]] = m[2]
			}
		}
	}
	return info
}
