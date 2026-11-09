package document

import "sort"

// Edit is a pending line-level change. Every edit is computed against the
// SAME untouched Lines slice, so all indices stay valid until ApplyEdits.
// Order breaks ties inside one Index (smaller runs first).
type Edit struct {
	Index  int
	Remove int
	Lines  []string
	Order  int
}

// ApplyEdits applies all edits from bottom to top. Grouping by Index and
// sorting descending is what makes previously computed indices remain valid.
func ApplyEdits(d *Document, edits []Edit) {
	if len(edits) == 0 {
		return
	}
	groups := map[int][]Edit{}
	for _, e := range edits {
		groups[e.Index] = append(groups[e.Index], e)
	}
	indices := make([]int, 0, len(groups))
	for k := range groups {
		indices = append(indices, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))

	lines := d.Lines
	for _, idx := range indices {
		g := groups[idx]
		sort.SliceStable(g, func(i, j int) bool { return g[i].Order < g[j].Order })
		remove := 0
		for _, e := range g {
			if e.Remove > remove {
				remove = e.Remove
			}
		}
		var inserted []string
		for _, e := range g {
			inserted = append(inserted, e.Lines...)
		}
		out := make([]string, 0, len(lines)-remove+len(inserted))
		out = append(out, lines[:idx]...)
		out = append(out, inserted...)
		out = append(out, lines[idx+remove:]...)
		lines = out
	}
	d.Lines = lines
}
