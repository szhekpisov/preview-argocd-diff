package render

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

// HelmOptions configures the offline Helm renderer.
type HelmOptions struct {
	// Runner executes `helm template`.
	Runner cluster.Runner
	// Namespace is passed via --namespace to match what ArgoCD would do at
	// sync time. Defaults to "default" when empty.
	Namespace string
	// IncludeCRDs passes --include-crds so that a chart's crds/ directory
	// participates in the diff just like ArgoCD would sync them.
	IncludeCRDs bool
	// SkipTests passes --skip-tests so chart tests don't appear in the diff.
	SkipTests bool
}

// Helm renders a Helm-based Application offline by shelling out to the helm
// CLI against a materialized tree. No cluster required.
type Helm struct {
	Opts HelmOptions
}

// NewHelm returns a new offline Helm renderer.
func NewHelm(opts HelmOptions) *Helm { return &Helm{Opts: opts} }

// Capable reports whether an app can be rendered offline. Helm-only
// Applications with an on-disk path qualify. Apps referencing a remote chart
// (spec.source.chart) require a registry fetch and are deferred to cluster
// mode. ApplicationSets are not rendered by this renderer.
func (h *Helm) Capable(app discover.Doc) (bool, string) {
	if app.Kind == discover.KindApplicationSet {
		return false, "ApplicationSet rendering not supported in offline mode"
	}
	src := primarySource(app.Spec)
	if src == nil {
		return false, "application has no source"
	}
	if src.Chart != "" && src.Path == "" {
		return false, fmt.Sprintf("remote chart %q not yet supported offline", src.Chart)
	}
	return true, ""
}

// Render executes `helm template` against the chart located at
// filepath.Join(treeDir, app.Spec.Source.Path) and returns its YAML output.
func (h *Helm) Render(ctx context.Context, app discover.Doc, _ string, treeDir string) ([]byte, error) {
	src := primarySource(app.Spec)
	if src == nil {
		return nil, fmt.Errorf("no source for %s", app.Name)
	}
	chartDir := filepath.Join(treeDir, filepath.FromSlash(src.Path))

	namespace := h.Opts.Namespace
	if namespace == "" {
		namespace = "default"
	}

	args := []string{"template", app.Name, chartDir, "--namespace", namespace}
	if h.Opts.IncludeCRDs {
		args = append(args, "--include-crds")
	}
	if h.Opts.SkipTests {
		args = append(args, "--skip-tests")
	}

	// Helm value files. Paths in spec.source.helm.valueFiles are relative to
	// spec.source.path by ArgoCD convention; we follow the same rule.
	// Absolute paths and $-ref multi-source syntax are handled explicitly.
	if src.Helm != nil {
		for _, vf := range src.Helm.ValueFiles {
			if strings.HasPrefix(vf, "$") {
				return nil, fmt.Errorf("helm valueFile %q uses $ref multi-source syntax, which requires cluster mode", vf)
			}
			resolved := vf
			if !filepath.IsAbs(vf) {
				resolved = filepath.Join(chartDir, filepath.FromSlash(vf))
			}
			args = append(args, "--values", resolved)
		}
		if src.Helm.Values != "" {
			return nil, fmt.Errorf("inline helm.values not yet implemented")
		}
	}

	res, err := h.Opts.Runner.Run(ctx, cluster.Command{Name: "helm", Args: args})
	if err != nil {
		return nil, fmt.Errorf("helm template %s: %w", app.Name, err)
	}
	return []byte(res.Stdout), nil
}

func primarySource(spec discover.AppSpec) *discover.Source {
	if spec.Source != nil {
		return spec.Source
	}
	if len(spec.Sources) > 0 {
		return &spec.Sources[0]
	}
	return nil
}

// Statically confirm Helm implements Renderer.
var _ Renderer = (*Helm)(nil)
