package changeset

import (
	"strings"
	"testing"

	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

func app(name, srcPath string, valueFiles ...string) discover.Doc {
	d := discover.Doc{
		Kind:      discover.KindApplication,
		Name:      name,
		Namespace: "argocd",
		File:      "apps/" + name + ".yaml",
		Spec: discover.AppSpec{
			Source: &discover.Source{
				RepoURL:        "https://example.com/r",
				Path:           srcPath,
				TargetRevision: "HEAD",
			},
		},
	}
	if len(valueFiles) > 0 {
		d.Spec.Source.Helm = &discover.HelmSource{ValueFiles: valueFiles}
	}
	return d
}

func TestBuildNoChanges(t *testing.T) {
	a := app("foo", "charts/foo")
	got := Build([]discover.Doc{a}, []discover.Doc{a}, nil)
	if len(got) != 0 {
		t.Fatalf("expected 0 changes, got %+v", got)
	}
}

func TestBuildSourcePathCovers(t *testing.T) {
	a := app("foo", "charts/foo")
	got := Build([]discover.Doc{a}, []discover.Doc{a}, []string{"charts/foo/templates/deploy.yaml"})
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
	if got[0].Key.Name != "foo" || got[0].Status != StatusModified {
		t.Errorf("unexpected: %+v", got[0])
	}
	if !strings.Contains(strings.Join(got[0].Reasons, "|"), "under source path") {
		t.Errorf("expected source-path reason, got %v", got[0].Reasons)
	}
}

func TestBuildUnrelatedChangeIgnored(t *testing.T) {
	a := app("foo", "charts/foo")
	b := app("bar", "charts/bar")
	got := Build(
		[]discover.Doc{a, b},
		[]discover.Doc{a, b},
		[]string{"charts/foo/templates/deploy.yaml"},
	)
	if len(got) != 1 || got[0].Key.Name != "foo" {
		t.Fatalf("only foo should be affected, got %+v", got)
	}
}

func TestBuildValueFileRelative(t *testing.T) {
	a := app("foo", "charts/foo", "values.yaml", "env/prod.yaml")
	got := Build(
		[]discover.Doc{a}, []discover.Doc{a},
		[]string{"charts/foo/env/prod.yaml"},
	)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
}

func TestBuildValueFileAbsolute(t *testing.T) {
	a := app("foo", "charts/foo", "shared/common.yaml")
	got := Build(
		[]discover.Doc{a}, []discover.Doc{a},
		[]string{"shared/common.yaml"},
	)
	// Absolute path relative to repo root — resolved form matches.
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
}

func TestBuildAppAddedAndRemoved(t *testing.T) {
	foo := app("foo", "charts/foo")
	bar := app("bar", "charts/bar")
	got := Build(
		[]discover.Doc{foo},       // base has only foo
		[]discover.Doc{bar},       // head has only bar
		[]string{"apps/bar.yaml"}, // a file did change, but the key point is presence diff
	)
	if len(got) != 2 {
		t.Fatalf("expected 2 changes, got %+v", got)
	}
	statuses := map[string]Status{}
	for _, c := range got {
		statuses[c.Key.Name] = c.Status
	}
	if statuses["foo"] != StatusRemoved {
		t.Errorf("foo status = %v", statuses["foo"])
	}
	if statuses["bar"] != StatusAdded {
		t.Errorf("bar status = %v", statuses["bar"])
	}
}

func TestBuildChartVersionBump(t *testing.T) {
	base := app("foo", "")
	base.Spec.Source.Chart = "nginx"
	base.Spec.Source.TargetRevision = "1.2.0"
	head := app("foo", "")
	head.Spec.Source.Chart = "nginx"
	head.Spec.Source.TargetRevision = "1.3.0"

	got := Build([]discover.Doc{base}, []discover.Doc{head}, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
	reasons := strings.Join(got[0].Reasons, "|")
	if !strings.Contains(reasons, "targetRevision") {
		t.Errorf("expected targetRevision reason, got %v", got[0].Reasons)
	}
}

func TestBuildCRDFileChanged(t *testing.T) {
	a := app("foo", "charts/foo")
	got := Build([]discover.Doc{a}, []discover.Doc{a}, []string{"apps/foo.yaml"})
	if len(got) != 1 || !strings.Contains(got[0].Reasons[0], "CRD file") {
		t.Fatalf("expected CRD-file reason, got %+v", got)
	}
}

func TestBuildAppSetTemplatePath(t *testing.T) {
	as := discover.Doc{
		Kind: discover.KindApplicationSet,
		Name: "fleet",
		File: "appsets/fleet.yaml",
		Spec: discover.AppSpec{
			Template: &discover.Template{
				Spec: discover.AppSpec{
					Source: &discover.Source{Path: "charts/tpl"},
				},
			},
			Generators: discover.GeneratorKinds{"list": true},
		},
	}
	got := Build([]discover.Doc{as}, []discover.Doc{as}, []string{"charts/tpl/values.yaml"})
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %+v", got)
	}
}
