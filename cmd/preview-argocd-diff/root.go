package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "preview-argocd-diff",
		Short:         "Render a diff of ArgoCD Applications affected by a PR and post it as a comment",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd(), newVersionCmd())
	return root
}
