// Package runner wires the swag invocation, the transformation pipeline and
// the output/statistics together. It is deliberately free of cobra so it can
// be exercised directly from tests.
package runner

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config is the complete set of options shared by the build and process
// commands. The zero value is not a valid config; cmd/ fills the defaults.
type Config struct {
	Root           string   // project root (swag must run there so .swaggo is picked up)
	Out            string   // output path relative to Root, or "-" for stdout
	Input          string   // process only: input file, or "-" for stdin
	EnumLabelField string   // description | name | both
	SkipSwag       bool     // build only: reuse an existing document instead of running swag
	KeepSwagger    bool     // build only: keep docs/swagger.yaml after a successful write
	Swag           string   // swag executable
	SwagArgs       []string // build only: arguments written after "--", passed through to swag init
	Enable         []string // enabled transformers
	StringEnums    bool     // also parse non-numeric enum values (ASC-升序 etc.)
	Strict         bool     // validate enum / x-apifox-enum consistency
	Color          string   // auto | always | never
	Quiet          bool
	Verbose        bool
}

// Defaults fills the fields the user did not set.
func (c *Config) Defaults() {
	if c.EnumLabelField == "" {
		c.EnumLabelField = "description"
	}
	if c.Swag == "" {
		c.Swag = "swag"
	}
	if len(c.Enable) == 0 {
		c.Enable = []string{"enum", "nullable"}
	}
	if c.Color == "" {
		c.Color = ColorAuto
	}
	switch c.EnumLabelField {
	case "description", "name", "both":
	default:
		// validated by Validate below
	}
}

func (c Config) Validate() error {
	switch c.EnumLabelField {
	case "description", "name", "both":
	default:
		return fmt.Errorf("invalid --enum-label-field %q (want description, name or both)", c.EnumLabelField)
	}
	switch c.Color {
	case ColorAuto, ColorAlways, ColorNever:
	default:
		return fmt.Errorf("invalid --color %q (want auto, always or never)", c.Color)
	}
	known := map[string]bool{"enum": true, "nullable": true}
	for _, name := range c.Enable {
		if !known[name] {
			return fmt.Errorf("unknown transformer %q in --enable", name)
		}
	}
	return nil
}

// ResolveRoot picks the project root: swag has to run there so that .swaggo
// and the general API annotations in main.go are visible. When no root is given we walk up from
// the current directory looking for main.go or .swaggo.
func ResolveRoot(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("root %q: %w", abs, err)
		}
		if !st.IsDir() {
			return "", fmt.Errorf("root %q is not a directory", abs)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "main.go")) || fileExists(filepath.Join(dir, ".swaggo")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// OutPath resolves --out against the project root. "-" means stdout.
func OutPath(cfg Config) string {
	if cfg.Out == "-" {
		return "-"
	}
	if filepath.IsAbs(cfg.Out) {
		return cfg.Out
	}
	return filepath.Join(cfg.Root, cfg.Out)
}
