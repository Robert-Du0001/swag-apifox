package document

import (
	"os"
	"reflect"
	"testing"
)

func TestParseBytesBOMAndNewlines(t *testing.T) {
	d := ParseBytes([]byte("\ufeffcomponents:\n  a: 1\n"))
	if d.NL != "\n" {
		t.Fatalf("NL = %q, want LF", d.NL)
	}
	if !reflect.DeepEqual(d.Lines, []string{"components:", "  a: 1", ""}) {
		t.Fatalf("lines = %#v", d.Lines)
	}
	if d.Text() != "components:\n  a: 1\n" {
		t.Fatalf("text = %q", d.Text())
	}

	d2 := ParseBytes([]byte("a: 1\r\nb: 2\r\n"))
	if d2.NL != "\r\n" {
		t.Fatalf("NL = %q, want CRLF", d2.NL)
	}
	if !reflect.DeepEqual(d2.Lines, []string{"a: 1", "b: 2", ""}) {
		t.Fatalf("lines = %#v", d2.Lines)
	}
	if d2.Text() != "a: 1\r\nb: 2\r\n" {
		t.Fatalf("CRLF round-trip failed: %q", d2.Text())
	}
}

func TestWriteFileNoBOM(t *testing.T) {
	path := t.TempDir() + "/nested/out.yaml"
	d := &Document{Lines: []string{"a: 1", ""}, NL: "\n"}
	if err := d.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	b := mustRead(t, path)
	if string(b) != "a: 1\n" {
		t.Fatalf("content = %q", b)
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		t.Fatal("output must not contain a BOM")
	}
}

func TestIndexNodesAndReadNode(t *testing.T) {
	lines := []string{
		"components:",
		"  schemas:",
		"    models.Status:",
		"      description: 状态",
		"      enum:",
		"      - 1",
		"      - 2",
		"      x-enum-comments:",
		"        A: 禁用",
		"        B: 启用",
		"      x-nullable: true",
		"",
		"info:",
		"  title: x",
	}
	var n Node
	found := false
	for _, cand := range IndexNodes(lines) {
		if cand.Key == "models.Status" {
			n, found = cand, true
			break
		}
	}
	if !found {
		t.Fatal("models.Status node not indexed")
	}
	if n.Start != 2 || n.Indent != 4 || n.End != 11 {
		t.Fatalf("node = %+v; want Start=2 Indent=4 End=11", n)
	}
	info := ReadNode(lines, n)
	if got := info.Lists["enum"]; !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("enum list = %#v (list items live at the same indent as their key)", got)
	}
	if info.Props["description"].Value != "状态" {
		t.Fatalf("description = %q", info.Props["description"].Value)
	}
	if info.Maps["x-enum-comments"]["A"] != "禁用" || info.Maps["x-enum-comments"]["B"] != "启用" {
		t.Fatalf("nested map = %#v", info.Maps["x-enum-comments"])
	}
	if _, ok := info.Props["x-nullable"]; !ok {
		t.Fatal("x-nullable must be a direct property")
	}
}

func TestReadNodeRecognisesRef(t *testing.T) {
	lines := []string{
		"      status:",
		"        $ref: '#/components/schemas/models.Status'",
		"        enum:",
		"        - 1",
		"        - 2",
	}
	n := Node{Start: 0, End: 4, Indent: 6, Key: "status"}
	info := ReadNode(lines, n)
	if info.Props["$ref"].Value != "'#/components/schemas/models.Status'" {
		t.Fatalf("$ref = %q", info.Props["$ref"].Value)
	}
	if len(info.Lists["enum"]) != 2 {
		t.Fatalf("enum = %#v", info.Lists["enum"])
	}
}

func TestScalarFormatting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1", "1"},
		{"-1.5", "-1.5"},
		{"true", "true"},
		{"null", "null"},
		{"'1'", "'1'"},
		{"\"ASC\"", "'ASC'"},
		{"ASC", "'ASC'"},
		{"it's", "'it''s'"},
	}
	for _, c := range cases {
		if got := FormatValue(c.in); got != c.want {
			t.Errorf("FormatValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	labels := []struct{ in, want string }{
		{"", "''"},
		{"禁用", "禁用"},
		{"a: b", "'a: b'"},
		{"-开头", "'-开头'"},
		{"含'引号", "'含''引号'"},
		{"a\\nb", "a b"},
	}
	for _, c := range labels {
		if got := FormatLabel(c.in); got != c.want {
			t.Errorf("FormatLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDescriptionTextBlockAndQuotes(t *testing.T) {
	lines := []string{
		"    node:",
		"      description: |",
		"        状态：1-禁用",
		"        2-启用",
		"      type: string",
	}
	n := Node{Start: 0, End: 4, Indent: 4}
	info := ReadNode(lines, n)
	if got := DescriptionText(lines, n, info); got != "状态：1-禁用\n2-启用" {
		t.Fatalf("block scalar = %q", got)
	}
	lines2 := []string{"  n:", "    description: \"1-禁用\\n2-启用\"", "    type: string"}
	n2 := Node{Start: 0, End: 2, Indent: 2}
	if got := DescriptionText(lines2, n2, ReadNode(lines2, n2)); got != "1-禁用\n2-启用" {
		t.Fatalf("double quoted = %q", got)
	}
}

func TestApplyEditsBottomUp(t *testing.T) {
	d := &Document{Lines: []string{"a", "b", "c", "d"}, NL: "\n"}
	ApplyEdits(d, []Edit{
		{Index: 1, Lines: []string{"X"}, Order: 1},
		{Index: 1, Lines: []string{"0"}, Order: 0},
		{Index: 3, Remove: 1, Lines: []string{"Y"}, Order: 2},
	})
	want := "a\n0\nX\nb\nc\nY"
	if d.Text() != want {
		t.Fatalf("text = %q, want %q", d.Text(), want)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
