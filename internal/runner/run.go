package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Robert-Du0001/swag-apifox/internal/document"
	"github.com/Robert-Du0001/swag-apifox/internal/transform"
)

// swagOutput locates the swag-generated source document. When swag is run it
// goes to a temp dir (so docs/swagger.yaml is never a deliverable) and the
// caller must invoke the returned cleanup function.
func swagOutput(cfg Config, log *swagLog) (string, func(), error) {
	if cfg.SkipSwag {
		for _, candidate := range []string{"docs/swagger.yaml", "docs/openapi.yaml"} {
			p := filepath.Join(cfg.Root, candidate)
			if fileExists(p) {
				return p, nil, nil
			}
		}
		return "", nil, errors.New("no swag output found. Run without --skip-swag")
	}
	dir, err := os.MkdirTemp("", "swag-apifox-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	args := swagCommandArgs(cfg, dir)
	cmd := exec.Command(cfg.Swag, args...)
	// swag must run at the project root: .swaggo and main.go annotations are
	// resolved relative to the working directory.
	cmd.Dir = cfg.Root
	// Stream swag's output as it arrives so a long parse never looks like a
	// hang. Both streams share one writer; swagLog serialises the writes.
	cmd.Stdout = log
	cmd.Stderr = log
	if cfg.Quiet {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "  swag command     : %s %s\n", cfg.Swag, strings.Join(args, " "))
	}
	// swag writes known const-evaluation warnings to stderr while exiting 0.
	// Only the process exit code decides success.
	runErr := cmd.Run()
	log.Flush()
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return "", cleanup, fmt.Errorf("swag init failed (exit %d)", ee.ExitCode())
		}
		return "", cleanup, fmt.Errorf("swag init: %w", runErr)
	}
	src := filepath.Join(dir, "swagger.yaml")
	if !fileExists(src) {
		return "", cleanup, fmt.Errorf("swag did not produce %s", src)
	}
	return src, cleanup, nil
}

// Transform applies the enabled transformers to a loaded document.
func Transform(doc *document.Document, cfg Config) (*transform.Stats, error) {
	tcfg := transform.Config{
		EnumLabelField: cfg.EnumLabelField,
		StringEnums:    cfg.StringEnums,
		Enable:         map[string]bool{},
	}
	for _, name := range cfg.Enable {
		tcfg.Enable[name] = true
	}
	registry := transform.NewRegistry(tcfg)
	// Strip first (idempotency), then index, then collect ALL edits against the
	// same untouched lines, and finally apply them bottom-up.
	registry.StripAll(doc)
	ctx := &transform.Context{
		Doc:         doc,
		Nodes:       document.IndexNodes(doc.Lines),
		Config:      tcfg,
		SchemaItems: transform.NewSchemaItems(),
		Stats:       &transform.Stats{},
	}
	edits, err := registry.ApplyAll(ctx)
	if err != nil {
		return nil, err
	}
	document.ApplyEdits(doc, edits)
	if cfg.Strict {
		if err := transform.Validate(doc); err != nil {
			return nil, err
		}
	}
	return ctx.Stats, nil
}

// Build is the one-shot flow: run swag -> post-process -> write docs/openapi.yaml.
func Build(cfg Config) error {
	color, restore := prepareColor(cfg.Color)
	defer restore()
	log := newSwagLog(os.Stderr, color)
	src, cleanup, err := swagOutput(cfg, log)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	doc, err := document.ReadFile(src)
	if err != nil {
		return err
	}
	stats, err := Transform(doc, cfg)
	if err != nil {
		return err
	}
	outPath := OutPath(cfg)
	if err := doc.WriteFile(outPath); err != nil {
		return err
	}
	if !cfg.KeepSwagger {
		_ = os.Remove(filepath.Join(cfg.Root, "docs/swagger.yaml"))
	}
	if !cfg.Quiet {
		PrintStats(os.Stderr, src, stats, cfg.Out, len(doc.Lines), color)
	}
	return nil
}

// Process post-processes an existing document without invoking swag.
func Process(cfg Config) error {
	color, restore := prepareColor(cfg.Color)
	defer restore()
	var (
		b   []byte
		err error
	)
	src := cfg.Input
	if src == "-" {
		src = "<stdin>"
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(src)
	}
	if err != nil {
		return err
	}
	doc := document.ParseBytes(b)
	stats, err := Transform(doc, cfg)
	if err != nil {
		return err
	}
	if cfg.Out == "-" {
		if _, err := io.WriteString(os.Stdout, doc.Text()); err != nil {
			return err
		}
	} else if err := doc.WriteFile(OutPath(cfg)); err != nil {
		return err
	}
	if !cfg.Quiet {
		PrintStats(os.Stderr, src, stats, cfg.Out, len(doc.Lines), color)
	}
	return nil
}

// PrintStats writes the statistics block to stderr so process' stdout stays a
// clean YAML document. Only the final "done." is tinted, when colour is on.
func PrintStats(w io.Writer, src string, st *transform.Stats, out string, lineCount int, color bool) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  source              : %s\n", src)
	fmt.Fprintf(w, "  x-nullable -> null  : %d\n", st.NullCount)
	fmt.Fprintf(w, "  schema enums        : %d\n", st.SchemaEnumCount)
	fmt.Fprintf(w, "  field enums         : %d\n", st.FieldEnumCount)
	fmt.Fprintf(w, "  output              : %s (%d lines)\n", out, lineCount)
	done := "done."
	if color {
		done = ansiGreen + done + ansiReset
	}
	fmt.Fprintf(w, "  %s\n", done)
}
