// Command preview-argocd-diff renders the diff of ArgoCD Applications
// affected by a pull request and posts the result as a comment on the PR.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
