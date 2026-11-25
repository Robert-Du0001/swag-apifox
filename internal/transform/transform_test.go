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

func exampleConfig() Config {
	return Config{EnumLabelField: "description", Enable: map[string]bool{"example": true}}
}

func TestParamExampleArrayBecomesList(t *testing.T) {
	lines := []string{
		"        schema:",
		"          items:",
		"            type: string",
		"          type: array",
		"          x-example: '[2026-10-01,2026-10-31]'",
		"        style: form",
	}
	d, _ := runTransform(t, lines, exampleConfig())
	// The example stays where x-example was: swag already sorted the keys,
	// and re-sorting would only churn the diff.
	want := strings.Join([]string{
		"        schema:",
		"          items:",
		"            type: string",
		"          type: array",
		"          example:",
		"          - '2026-10-01'",
		"          - '2026-10-31'",
		"        style: form",
	}, "\n")
	if d.Text() != want {
		t.Fatalf("got://n%s//nwant://n%s", d.Text(), want)
	}
}

func TestParamExampleScalarKeepsType(t *testing.T) {
	lines := []string{
		"          type: array",
		"          x-example: '[1,2]'",
		"        style: form",
	}
	d, _ := runTransform(t, lines, exampleConfig())
	text := d.Text()
	if !strings.Contains(text, "          - 1\n          - 2") {
		t.Fatalf("numeric items must stay numbers:/n%s", text)
	}

	d, _ = runTransform(t, []string{"        x-example: DESC"}, exampleConfig())
	if got := d.Text(); got != "        example: 'DESC'" {
		t.Fatalf("scalar: got %q", got)
	}

	d, _ = runTransform(t, []string{"        x-example: 1"}, exampleConfig())
	if got := d.Text(); got != "        example: 1" {
		t.Fatalf("numeric scalar: got %q", got)
	}
}

func TestParamExampleKeepsRealExample(t *testing.T) {
	lines := []string{
		"        schema:",
		"          example: real",
		"          type: string",
		"          x-example: ignored",
	}
	d, _ := runTransform(t, lines, exampleConfig())
	want := strings.Join([]string{
		"        schema:",
		"          example: real",
		"          type: string",
	}, "\n")
	if d.Text() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", d.Text(), want)
	}
}

func TestParamExampleIsIdempotent(t *testing.T) {
	lines := []string{
		"        schema:",
		"          type: array",
		"          x-example: '[a,b]'",
	}
	first, _ := runTransform(t, lines, exampleConfig())
	second, _ := runTransform(t, first.Lines, exampleConfig())
	if first.Text() != second.Text() {
		t.Fatalf("not idempotent:/nfirst:/n%s/nsecond:/n%s", first.Text(), second.Text())
	}
}

func TestOperationDescriptionNotTurnedIntoEnum(t *testing.T) {
	// An operation's description mentioning "200 成功，400 订单不存在" must
	// never produce operation-level enum / x-apifox-enum blocks.
	lines := []string{
		"  /office/user/space:",
		"    get:",
		"      description: 官网内网调用，HTTP 恒 200，code=200 成功，code=400 订单不存在，code=500 服务端错误。",
		"      responses:",
		"        '200':",
		"          description: ok",
		"      summary: 获取购买订单的空间数据",
	}
	d, _ := runTransform(t, lines, baseConfig("description"))
	got := d.Text()
	if strings.Contains(got, "enum:") || strings.Contains(got, "x-apifox-enum") {
		t.Fatalf("operation description must not become enum:/n%s", got)
	}
}

func TestStripRemovesStaleOperationEnum(t *testing.T) {
	// A previously mis-generated operation-level enum must be stripped so
	// re-running the tool is idempotent.
	lines := []string{
		"  /office/user/space:",
		"    get:",
		"      description: 官网内网调用，HTTP 恒 200，code=200 成功，code=400 订单不存在，code=500 服务端错误。",
		"      responses:",
		"        '200':",
		"          description: ok",
		"      summary: 获取购买订单的空间数据",
		"      enum:",
		"      - 200",
		"      - 400",
		"      x-apifox-enum:",
		"        - value: 200",
		"          name: ''",
		"          description: 成功",
		"        - value: 400",
		"          name: ''",
		"          description: 订单不存在",
	}
	d, _ := runTransform(t, lines, baseConfig("description"))
	got := d.Text()
	if strings.Contains(got, "enum:") || strings.Contains(got, "x-apifox-enum") {
		t.Fatalf("stale operation enum not stripped:/n%s", got)
	}
}
