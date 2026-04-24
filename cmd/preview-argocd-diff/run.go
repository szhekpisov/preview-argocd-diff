package main

import (
	"github.com/spf13/cobra"

	"github.com/szhekpisov/preview-argocd-diff/internal/config"
	"github.com/szhekpisov/preview-argocd-diff/internal/pipeline"
)

func newRunCmd() *cobra.Command {
	cfg := config.New()
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the diff pipeline against the current repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cfg.Load(cmd); err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			return pipeline.Run(cmd.Context(), cfg, pipeline.Deps{})
		},
	}
	cfg.BindFlags(cmd.Flags())
	return cmd
}
