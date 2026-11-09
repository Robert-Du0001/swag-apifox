package transform

import (
	"strings"
	"testing"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
)

func baseConfig(field string) Config {
	return Config{
		EnumLabelField: field,
		Enable:         map[string]bool{"enum": true, "nullable": true},
	}
}

func runTransform(t *testing.T, lines []string, cfg Config) (*document.Document, *Stats) {
	t.Helper()
	d := &document.Document{Lines: append([]string(nil), lines...), NL: "\n"}
	r := NewRegistry(cfg)
	r.StripAll(d)
	ctx := &Context{
		Doc:         d,
		Nodes:       document.IndexNodes(d.Lines),
		Config:      cfg,
		SchemaItems: NewSchemaItems(),
		Stats:       &Stats{},
	}
	edits, err := r.ApplyAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	document.ApplyEdits(d, edits)
	return d, ctx.Stats
}

func TestNullableRebuild(t *testing.T) {
	lines := []string{
		"data:",
		"  $ref: '#/components/schemas/X'",
		"  description: 恒为 null",
		"  title: 数据",
		"  type: string",
		"  x-nullable: true",
	}
	d, st := runTransform(t, lines, baseConfig("description"))
	want := "data:\n  description: 恒为 null\n  title: 数据\n  type: 'null'"
	if d.Text() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", d.Text(), want)
	}
	if st.NullCount != 1 {
		t.Fatalf("NullCount = %d, want 1", st.NullCount)
	}
}

var enumNode = []string{
	"    models.Status:",
	"      description: 状态：1-禁用，2-启用",
	"      enum:",
	"      - 1",
	"      - 2",
	"      type: integer",
}

func TestEnumLabelField(t *testing.T) {
	for _, tc := range []struct {
		field string
		name  string
		desc  string
	}{
		{"description", "name: ''", "description: 禁用"},
		{"name", "name: 禁用", "description: ''"},
		{"both", "name: 禁用", "description: 禁用"},
	} {
		d, st := runTransform(t, enumNode, baseConfig(tc.field))
		text := d.Text()
		if !strings.Contains(text, "- value: 1") || !strings.Contains(text, tc.name) || !strings.Contains(text, tc.desc) {
			t.Errorf("field %q missing expected lines:\n%s", tc.field, text)
		}
		if st.FieldEnumCount != 1 {
			t.Errorf("field %q: FieldEnumCount = %d, want 1", tc.field, st.FieldEnumCount)
		}
	}
}

func TestSmallDocumentIsIdempotent(t *testing.T) {
	first, _ := runTransform(t, enumNode, baseConfig("description"))
	second, _ := runTransform(t, first.Lines, baseConfig("description"))
	if first.Text() != second.Text() {
		t.Fatalf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first.Text(), second.Text())
	}
}

func TestStripOnlyRemovesApifoxBlock(t *testing.T) {
	lines := []string{
		"  status:",
		"    enum:",
		"    - 1",
		"    - 2",
		"    x-apifox-enum:",
		"      - value: 1",
		"        name: ''",
		"        description: 禁用",
		"    type: integer",
	}
	d := &document.Document{Lines: append([]string(nil), lines...), NL: "\n"}
	(&ApifoxEnum{}).Strip(d)
	text := d.Text()
	if strings.Contains(text, "x-apifox-enum") || !strings.Contains(text, "enum:") {
		t.Fatalf("strip must keep enum: and drop x-apifox-enum:\n%s", text)
	}
	if !strings.Contains(text, "    - 2") {
		t.Fatalf("enum values were damaged:\n%s", text)
	}
}
