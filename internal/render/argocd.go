package render

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

// ArgoCDOptions configures the cluster-mode renderer.
type ArgoCDOptions struct {
	// Runner is the command executor. Production uses cluster.NewExecRunner().
	Runner cluster.Runner
	// KubeconfigPath is exported as $KUBECONFIG to every invocation.
	KubeconfigPath string
	// ArgoCDNamespace is where the Application CRDs are applied. Matches the
	// namespace the Argo CD install lives in — usually "argocd".
	ArgoCDNamespace string
	// RepoURL is the HTTPS clone URL of the repo under review. Applications
	// are patched to point at this URL.
	RepoURL string
	// DestinationNamespace is what the rendered app's spec.destination.namespace
	// is forced to. For render-only use, any non-empty name works.
	DestinationNamespace string
}

// ArgoCD is a cluster-mode Renderer that applies patched Application CRDs
// and extracts manifests via the argocd CLI's --core mode (which speaks to
// the API through KUBECONFIG, avoiding a separate login step).
type ArgoCD struct {
	Opts ArgoCDOptions
}

// NewArgoCD returns a fresh renderer.
func NewArgoCD(opts ArgoCDOptions) *ArgoCD {
	if opts.ArgoCDNamespace == "" {
		opts.ArgoCDNamespace = "argocd"
	}
	if opts.DestinationNamespace == "" {
		opts.DestinationNamespace = "default"
	}
	return &ArgoCD{Opts: opts}
}

// Capable decides whether the renderer can handle the given app. ApplicationSets
// with generators that need real external state (clusters/scmProvider/pullRequest)
// are rejected with a reason the report surfaces.
func (r *ArgoCD) Capable(app discover.Doc) (bool, string) {
	if app.Kind != discover.KindApplicationSet {
		return true, ""
	}
	for g := range app.Spec.Generators {
		switch g {
		case "clusters", "scmProvider", "pullRequest":
			return false, fmt.Sprintf("ApplicationSet generator %q requires additional cluster setup; skipped in this version", g)
		}
	}
	return true, ""
}

// Render applies a patched copy of the Application to the cluster and extracts
// its rendered manifests at the provided ref. The treeDir argument is ignored
// — cluster-mode rendering delegates to Argo CD's repo-server, which fetches
// directly from the Application's repoURL.
func (r *ArgoCD) Render(ctx context.Context, app discover.Doc, ref, _ string) ([]byte, error) {
	if app.Kind == discover.KindApplicationSet {
		// AppSet rendering requires cluster-side generator expansion plus
		// retrieving each generated Application's manifests. Deferred to
		// v0.2 when offline generator expansion lands.
		return nil, fmt.Errorf("ApplicationSet rendering not implemented in this version (app=%s)", app.Name)
	}

	patched := r.patchApp(app, ref)
	if _, err := r.Opts.Runner.Run(ctx, cluster.Command{
		Name:  "kubectl",
		Args:  []string{"apply", "-n", r.Opts.ArgoCDNamespace, "-f", "-"},
		Env:   r.env(),
		Stdin: bytes.NewReader([]byte(patched)),
	}); err != nil {
		return nil, fmt.Errorf("apply app %s: %w", app.Name, err)
	}

	// Force ArgoCD to pull the ref from the repo-server and populate its
	// manifest cache. Without this, `argocd app manifests` races with the
	// application-controller's first reconciliation and fails with
	// "cache: key is missing".
	if _, err := r.Opts.Runner.Run(ctx, cluster.Command{
		Name: "argocd",
		Args: []string{"app", "get", "--core", app.Name, "--hard-refresh"},
		Env:  r.env(),
	}); err != nil {
		return nil, fmt.Errorf("argocd refresh %s: %w", app.Name, err)
	}

	res, err := r.Opts.Runner.Run(ctx, cluster.Command{
		Name: "argocd",
		Args: []string{
			"app", "manifests", "--core", app.Name,
			"--revision", ref,
		},
		Env: r.env(),
	})
	if err != nil {
		return nil, fmt.Errorf("argocd manifests %s: %w", app.Name, err)
	}
	return []byte(res.Stdout), nil
}

func (r *ArgoCD) env() []string {
	var env []string
	if r.Opts.KubeconfigPath != "" {
		env = append(env, "KUBECONFIG="+r.Opts.KubeconfigPath)
	}
	// ARGOCD_NAMESPACE is read by the argocd CLI in --core mode to locate
	// argocd-cm and other config objects. Without it, the CLI looks in the
	// kubeconfig's current-context namespace (typically "default") and
	// errors with "configmap 'argocd-cm' not found".
	if r.Opts.ArgoCDNamespace != "" {
		env = append(env, "ARGOCD_NAMESPACE="+r.Opts.ArgoCDNamespace)
	}
	return env
}

// patchApp produces the YAML applied to the cluster: the app's spec is kept
// but the source's targetRevision is replaced with ref and any syncPolicy is
// stripped so no live resources are actually reconciled.
func (r *ArgoCD) patchApp(app discover.Doc, ref string) string {
	repoURL := r.Opts.RepoURL
	sourcePath := ""
	chart := ""
	valueFiles := ""
	if app.Spec.Source != nil {
		if app.Spec.Source.RepoURL != "" {
			repoURL = app.Spec.Source.RepoURL
		}
		sourcePath = app.Spec.Source.Path
		chart = app.Spec.Source.Chart
		if app.Spec.Source.Helm != nil && len(app.Spec.Source.Helm.ValueFiles) > 0 {
			var lines []string
			for _, vf := range app.Spec.Source.Helm.ValueFiles {
				lines = append(lines, "        - "+vf)
			}
			valueFiles = "      helm:\n        valueFiles:\n" + strings.Join(lines, "\n") + "\n"
		}
	}

	var src strings.Builder
	src.WriteString("    repoURL: " + repoURL + "\n")
	src.WriteString("    targetRevision: " + ref + "\n")
	if chart != "" {
		src.WriteString("    chart: " + chart + "\n")
	}
	if sourcePath != "" {
		src.WriteString("    path: " + sourcePath + "\n")
	}
	if valueFiles != "" {
		src.WriteString(valueFiles)
	}

	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
  project: default
  destination:
    server: https://kubernetes.default.svc
    namespace: %s
  source:
%s`, app.Name, r.Opts.ArgoCDNamespace, r.Opts.DestinationNamespace, src.String())
}

// Statically confirm ArgoCD implements Renderer.
var _ Renderer = (*ArgoCD)(nil)
