// Package render turns an Application / ApplicationSet doc into rendered
// Kubernetes manifests for a given git ref. The MVP uses a live ArgoCD inside
// a Kind cluster; a pure `helm template` renderer is planned for v0.3.
package render

import (
	"context"

	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

// Renderer is the rendering strategy applied to one app + one ref.
type Renderer interface {
	// Render returns the rendered YAML as a byte slice, with multi-document
	// separators preserved.
	Render(ctx context.Context, app discover.Doc, ref string) ([]byte, error)

	// Capable reports whether this renderer can handle the given app. The
	// returned reason is used in the PR report when the renderer declines.
	Capable(app discover.Doc) (bool, string)
}
