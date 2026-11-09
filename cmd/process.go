package cmd

import (
	"github.com/Robert-Du0001/swag-apifox/internal/runner"
	"github.com/spf13/cobra"
)

var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Post-process an existing swag YAML (file or stdin) without running swag",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg.Out == "" {
			cfg.Out = "-"
		}
		if err := prepare(); err != nil {
			return err
		}
		return runner.Process(cfg)
	},
}

func init() {
	processCmd.Flags().StringVarP(&cfg.Input, "input", "i", "-", "input YAML file, or - for stdin")
	rootCmd.AddCommand(processCmd)
}
