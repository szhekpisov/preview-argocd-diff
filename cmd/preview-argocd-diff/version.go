package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, build date, and Go runtime",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("preview-argocd-diff %s (commit %s, built %s, %s/%s)\n",
				version, commit, date, runtime.GOOS, runtime.GOARCH)
		},
	}
}
