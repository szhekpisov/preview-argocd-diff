package render

import (
	"context"
	"strings"
	"testing"

	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

type recRunner struct {
	calls []cluster.Command
	out   map[string]string
}

func (r *recRunner) Run(_ context.Context, c cluster.Command) (cluster.Result, error) {
	r.calls = append(r.calls, c)
	key := c.Name + " " + strings.Join(c.Args, " ")
	return cluster.Result{Stdout: r.out[key]}, nil
}

func TestArgoCDCapable(t *testing.T) {
	r := NewArgoCD(ArgoCDOptions{})

	if ok, _ := r.Capable(discover.Doc{Kind: discover.KindApplication}); !ok {
		t.Error("should render Application")
	}

	appset := discover.Doc{
		Kind: discover.KindApplicationSet,
		Spec: discover.AppSpec{Generators: discover.GeneratorKinds{"list": true}},
	}
	if ok, _ := r.Capable(appset); ok {
		// ApplicationSets are deferred even if generator is supported offline —
		// Render returns the deferred error. Capable is allowed to return
		// true; we want the capability gate at render time.
		_ = ok
	}

	cluster := discover.Doc{
		Kind: discover.KindApplicationSet,
		Spec: discover.AppSpec{Generators: discover.GeneratorKinds{"clusters": true}},
	}
	if ok, reason := r.Capable(cluster); ok || reason == "" {
		t.Errorf("cluster-generator AppSet should be incapable, got ok=%v reason=%q", ok, reason)
	}
}

func TestArgoCDRenderAppliesAndFetches(t *testing.T) {
	rr := &recRunner{out: map[string]string{
		"argocd app manifests --core foo --revision abc123": "kind: ConfigMap\nmetadata:\n  name: rendered\n",
	}}
	r := NewArgoCD(ArgoCDOptions{
		Runner:         rr,
		KubeconfigPath: "/tmp/kc",
		RepoURL:        "https://example.com/repo",
	})

	app := discover.Doc{
		Kind:      discover.KindApplication,
		Name:      "foo",
		Namespace: "argocd",
		Spec: discover.AppSpec{
			Source: &discover.Source{
				Path: "charts/foo",
				Helm: &discover.HelmSource{ValueFiles: []string{"values.yaml"}},
			},
		},
	}
	out, err := r.Render(context.Background(), app, "abc123")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(out), "rendered") {
		t.Errorf("unexpected manifests: %q", out)
	}

	if len(rr.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(rr.calls), rr.calls)
	}
	apply := rr.calls[0]
	if apply.Name != "kubectl" || !strings.Contains(strings.Join(apply.Args, " "), "apply") {
		t.Errorf("first call should be kubectl apply, got %+v", apply)
	}

	// Ensure the piped manifest carries the correct repo/rev and source path.
	if apply.Stdin == nil {
		t.Fatal("kubectl apply should receive stdin")
	}
	buf := make([]byte, 4096)
	n, _ := apply.Stdin.Read(buf)
	body := string(buf[:n])
	for _, want := range []string{"repoURL: https://example.com/repo", "targetRevision: abc123", "path: charts/foo", "values.yaml"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in applied manifest:\n%s", want, body)
		}
	}
}
