package runner

import (
	"strings"
	"testing"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
	"github.com/Robert-Du0001/swag-apifox/internal/transform"
)

// sampleYAML is an inline minimal swag output covering all three steps:
// schema enum, field/parameter enum and x-nullable. Keeping it inline means
// the test suite needs no fixture files on disk.
const sampleYAML = `components:
  schemas:
    models.Status:
      description: 状态：1-禁用，2-启用
      enum:
      - 1
      - 2
      type: integer
      x-enum-comments:
        StatusEnabled: 启用
        StatusDisabled: 禁用
      x-enum-varnames:
      - StatusDisabled
      - StatusEnabled
    dto.NullResponse:
      properties:
        data:
          description: 恒为 null
          type: object
          x-nullable: true
      type: object
paths:
  /companies:
    get:
      parameters:
      - description: 类型：0-全部，1-日常收支，2-内部转账
        in: query
        name: bus_type
        schema:
          enum:
          - 0
          - 1
          - 2
          type: integer
      responses:
        '200':
          description: ok
`

func defaultConfig() Config {
	return Config{EnumLabelField: "description", Enable: []string{"enum", "nullable"}}
}

func newDoc(text string) *document.Document {
	return &document.Document{Lines: strings.Split(text, "\n"), NL: "\n"}
}

func transformText(t *testing.T, text string, cfg Config) (*document.Document, *transform.Stats) {
	t.Helper()
	doc := newDoc(text)
	stats, err := Transform(doc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return doc, stats
}

// TestEndToEnd exercises the whole pipeline without touching the filesystem.
func TestEndToEnd(t *testing.T) {
	doc, stats := transformText(t, sampleYAML, defaultConfig())
	text := doc.Text()
	for _, want := range []string{
		"description: 禁用", // Step 1 aligns by x-enum-varnames, not map order
		"description: 启用",
		"description: 全部", // Step 2 walks back to the parent parameter description
		"type: 'null'",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in output:\n%s", want, text)
		}
	}
	if strings.Contains(text, "x-nullable") {
		t.Error("x-nullable was not replaced")
	}
	if stats.SchemaEnumCount != 1 || stats.NullCount != 1 || stats.FieldEnumCount < 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// TestIsIdempotent: processing an already transformed document again must
// produce byte-identical output.
func TestIsIdempotent(t *testing.T) {
	first, _ := transformText(t, sampleYAML, defaultConfig())
	want := first.Text()
	second := newDoc(want)
	if _, err := Transform(second, defaultConfig()); err != nil {
		t.Fatal(err)
	}
	if second.Text() != want {
		t.Fatalf("re-processing changed the document:\nfirst:\n%s\nsecond:\n%s", want, second.Text())
	}
}

func TestEnumLabelFieldVariants(t *testing.T) {
	cfg := defaultConfig()
	cfg.EnumLabelField = "name"
	doc, _ := transformText(t, sampleYAML, cfg)
	if !strings.Contains(doc.Text(), "name: 禁用") || !strings.Contains(doc.Text(), "description: ''") {
		t.Fatal("EnumLabelField=name did not move the Chinese label")
	}
}

func TestValidateDetectsMismatch(t *testing.T) {
	bad := newDoc(`  status:
    enum:
    - 1
    - 2
    x-apifox-enum:
      - value: 1
        name: ''
        description: 禁用
`)
	if err := transform.Validate(bad); err == nil {
		t.Fatal("expected an error for enum/x-apifox-enum count mismatch")
	}
}
