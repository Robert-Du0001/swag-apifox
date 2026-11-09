// Package transform holds the post-processing rules. Each rule is a
// Transformer with two phases: Strip (make the document idempotent before
// indexing) and Apply (collect line edits against the untouched document).
package transform

import (
	"github.com/Robert-Du0001/swag-apifox/internal/document"
)

// Config is the transform-layer view of the CLI options.
type Config struct {
	EnumLabelField string
	StringEnums    bool
	Enable         map[string]bool
}

// Item is one enum value and its Chinese label.
type Item struct {
	Value string
	Label string
}

// Stats mirrors the five-line statistics block of the original script.
type Stats struct {
	NullCount       int
	SchemaEnumCount int
	FieldEnumCount  int
}

// SchemaItems keeps the labels of schema-level enums in insertion order.
// Step 1 fills it, Step 2 reuses it through $ref and best-match.
type SchemaItems struct {
	keys []string
	m    map[string][]Item
}

func NewSchemaItems() *SchemaItems {
	return &SchemaItems{m: map[string][]Item{}}
}

func (s *SchemaItems) Set(k string, items []Item) {
	if _, ok := s.m[k]; !ok {
		s.keys = append(s.keys, k)
	}
	s.m[k] = items
}

func (s *SchemaItems) Get(k string) []Item { return s.m[k] }
func (s *SchemaItems) Keys() []string      { return s.keys }

// Context is shared by all transformers during one run.
type Context struct {
	Doc         *document.Document
	Nodes       []document.Node
	Config      Config
	SchemaItems *SchemaItems
	Stats       *Stats
}

// Transformer is the extension point for new x-apifox-* fields.
type Transformer interface {
	Name() string
	Strip(doc *document.Document)
	Apply(ctx *Context) ([]document.Edit, error)
}

// Registry runs transformers in a fixed order so Edit.Order stays stable.
type Registry struct {
	ts []Transformer
}

func NewRegistry(cfg Config) *Registry {
	r := &Registry{}
	if cfg.Enable["enum"] {
		r.Register(&ApifoxEnum{cfg: cfg})
	}
	if cfg.Enable["nullable"] {
		r.Register(&Nullable{})
	}
	return r
}

func (r *Registry) Register(t Transformer) { r.ts = append(r.ts, t) }

func (r *Registry) StripAll(doc *document.Document) {
	for _, t := range r.ts {
		t.Strip(doc)
	}
}

func (r *Registry) ApplyAll(ctx *Context) ([]document.Edit, error) {
	var all []document.Edit
	for _, t := range r.ts {
		edits, err := t.Apply(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, edits...)
	}
	return all, nil
}
