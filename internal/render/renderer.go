// Package render turns an Application / ApplicationSet doc into rendered
// Kubernetes manifests for a given git ref. Two strategies are supported:
//
//   - Helm: offline `helm template` against a materialized tree (default).
//   - ArgoCD: live ArgoCD in a Kind cluster, used when an Application's
//     source can't be evaluated offline.
package render

import (
	"context"

	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

// Renderer is the rendering strategy applied to one app + one ref.
//
// treeDir is the path to a materialized copy of the repo at ref, as produced
// by git.Repo.Materialize. Offline renderers (Helm) read from it; cluster-mode
// renderers (ArgoCD) ignore it.
type Renderer interface {
	Render(ctx context.Context, app discover.Doc, ref, treeDir string) ([]byte, error)

	// Capable reports whether this renderer can handle the given app. The
	// returned reason is used in the PR report when the renderer declines.
	Capable(app discover.Doc) (bool, string)
}
