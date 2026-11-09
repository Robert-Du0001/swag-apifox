package cmd

import (
	"fmt"

	"github.com/Robert-Du0001/swag-apifox/internal/runner"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build [flags] [-- <swag args>...]",
	Short: "Run swag init into a temp dir, post-process and write docs/openapi.yaml",
	Long: "Run `swag init --v3.1 --ot yaml`, post-process its output and write docs/openapi.yaml.\n" +
		"Anything after `--` is passed straight through to swag, e.g.\n" +
		"`swag-apifox build -- --parseDependency --parseDepth 1`.",
	Example: `  swag-apifox build
  swag-apifox build -- "--parseDependency --parseDepth 1"
  swag-apifox build -- --parseDependency --parseDepth 1`,
	// pflag stops parsing flags at "--" and hands the remainder over as
	// positional args. Those are the swag options; anything before "--" is a
	// mistake (most likely a forgotten "--").
	Args: func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		if dash < 0 {
			if len(args) > 0 {
				return fmt.Errorf("unexpected argument %q: swag options must come after --", args[0])
			}
			return nil
		}
		if dash > 0 {
			return fmt.Errorf("unexpected argument %q before --", args[0])
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			passthrough, err := runner.PassthroughArgs(args[dash:])
			if err != nil {
				return err
			}
			cfg.SwagArgs = passthrough
			if cfg.SkipSwag && len(passthrough) > 0 {
				return fmt.Errorf("swag options were given after --, but --skip-swag does not run swag")
			}
		}
		if cfg.Out == "" {
			cfg.Out = "docs/openapi.yaml"
		}
		if err := prepare(); err != nil {
			return err
		}
		return runner.Build(cfg)
	},
}

func init() {
	buildCmd.Flags().BoolVar(&cfg.SkipSwag, "skip-swag", false, "reuse docs/swagger.yaml or docs/openapi.yaml instead of running swag")
	buildCmd.Flags().BoolVar(&cfg.KeepSwagger, "keep-swagger", false, "keep docs/swagger.yaml after a successful write")
	rootCmd.AddCommand(buildCmd)
}
