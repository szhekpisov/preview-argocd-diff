package render

import (
	"context"
	"strings"
	"testing"

	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

func TestHelmCapable(t *testing.T) {
	r := NewHelm(HelmOptions{})

	cases := []struct {
		name string
		doc  discover.Doc
		ok   bool
	}{
		{"local-helm", discover.Doc{Kind: discover.KindApplication, Spec: discover.AppSpec{Source: &discover.Source{Path: "charts/foo"}}}, true},
		{"appset", discover.Doc{Kind: discover.KindApplicationSet}, false},
		{"no-source", discover.Doc{Kind: discover.KindApplication}, false},
		{"remote-chart", discover.Doc{Kind: discover.KindApplication, Spec: discover.AppSpec{Source: &discover.Source{Chart: "nginx"}}}, false},
	}
	for _, tc := range cases {
		got, _ := r.Capable(tc.doc)
		if got != tc.ok {
			t.Errorf("%s: capable = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

func TestHelmRenderArgs(t *testing.T) {
	rr := &recRunner{out: map[string]string{}}
	r := NewHelm(HelmOptions{
		Runner:      rr,
		Namespace:   "argocd",
		IncludeCRDs: true,
		SkipTests:   true,
	})

	app := discover.Doc{
		Kind: discover.KindApplication,
		Name: "foo",
		Spec: discover.AppSpec{
			Source: &discover.Source{
				Path: "charts/foo",
				Helm: &discover.HelmSource{ValueFiles: []string{"values.yaml", "envs/prod.yaml"}},
			},
		},
	}

	if _, err := r.Render(context.Background(), app, "abc123", "/tmp/tree"); err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(rr.calls) != 1 {
		t.Fatalf("expected 1 helm call, got %d", len(rr.calls))
	}
	got := rr.calls[0]
	if got.Name != "helm" {
		t.Errorf("command = %s, want helm", got.Name)
	}
	args := strings.Join(got.Args, " ")
	for _, want := range []string{
		"template foo /tmp/tree/charts/foo",
		"--namespace argocd",
		"--include-crds",
		"--skip-tests",
		"--values /tmp/tree/charts/foo/values.yaml",
		"--values /tmp/tree/charts/foo/envs/prod.yaml",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in helm args:\n%s", want, args)
		}
	}
}

func TestHelmRenderRejectsRefValues(t *testing.T) {
	r := NewHelm(HelmOptions{Runner: &recRunner{}})
	app := discover.Doc{
		Kind: discover.KindApplication,
		Name: "foo",
		Spec: discover.AppSpec{
			Source: &discover.Source{
				Path: "charts/foo",
				Helm: &discover.HelmSource{ValueFiles: []string{"$values/shared.yaml"}},
			},
		},
	}
	_, err := r.Render(context.Background(), app, "abc", "/tmp/tree")
	if err == nil || !strings.Contains(err.Error(), "$ref multi-source") {
		t.Errorf("expected $ref rejection, got %v", err)
	}
}

// Re-use recRunner from argocd_test.go to avoid duplication. Confirm at
// compile time that it implements cluster.Runner.
var _ cluster.Runner = (*recRunner)(nil)
