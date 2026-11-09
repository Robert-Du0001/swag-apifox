// Package cmd contains the cobra command tree for swag-apifox.
package cmd

import (
	"fmt"
	"os"

	"github.com/Robert-Du0001/swag-apifox/internal/runner"
	"github.com/spf13/cobra"
)

var Version = "0.1.0"

var cfg runner.Config

var rootCmd = &cobra.Command{
	Use:           "swag-apifox",
	Short:         "Post-process swag OpenAPI output for Apifox",
	Long:          "swag-apifox runs swag v2 and applies text-level fixes to its OpenAPI 3.1 YAML:\nx-nullable -> type: 'null', and enum metadata -> x-apifox-enum.\nIt never re-serializes the document, so swag's indentation, key order and quoting survive.",
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       Version,
}

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&cfg.Root, "root", "", "project root (default: walk up from cwd looking for main.go/.swaggo)")
	f.StringVarP(&cfg.Out, "out", "o", "", "output path relative to root, or - for stdout (build: docs/openapi.yaml, process: -)")
	f.StringVar(&cfg.EnumLabelField, "enum-label-field", "description", "where to write the Chinese enum label: description, name or both")
	f.StringVar(&cfg.Swag, "swag", "swag", "swag executable (PATH name or absolute path)")
	f.StringArrayVar(&cfg.Enable, "enable", nil, "transformer to enable (repeatable; default: enum,nullable)")
	f.BoolVar(&cfg.StringEnums, "string-enums", false, "also parse non-numeric enum values such as ASC-升序")
	f.BoolVar(&cfg.Strict, "strict", false, "fail when enum and x-apifox-enum disagree")
	f.BoolVar(&cfg.Quiet, "quiet", false, "suppress swag output and the statistics block")
	f.BoolVar(&cfg.Verbose, "verbose", false, "also print the full swag command line")
	f.StringVar(&cfg.Color, "color", "auto", "colourise swag output: auto, always or never")
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// prepare applies defaults, resolves the root and validates the flags.
func prepare() error {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	root, err := runner.ResolveRoot(cfg.Root)
	if err != nil {
		return err
	}
	cfg.Root = root
	return nil
}
