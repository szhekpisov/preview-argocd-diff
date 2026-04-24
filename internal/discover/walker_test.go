package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkDecodesApplication(t *testing.T) {
	root := t.TempDir()
	write(t, root, "apps/foo.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: foo
  namespace: argocd
  labels:
    team: infra
spec:
  source:
    repoURL: https://example.com/repo
    path: charts/foo
    targetRevision: HEAD
    helm:
      valueFiles:
        - values.yaml
        - env/prod.yaml
`)

	docs, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("got %d docs, want 1", len(docs))
	}
	d := docs[0]
	if d.Kind != KindApplication {
		t.Errorf("kind = %q", d.Kind)
	}
	if d.Name != "foo" {
		t.Errorf("name = %q", d.Name)
	}
	if d.File != "apps/foo.yaml" {
		t.Errorf("file = %q", d.File)
	}
	if d.Labels["team"] != "infra" {
		t.Errorf("labels = %v", d.Labels)
	}
	if d.Spec.Source == nil || d.Spec.Source.Path != "charts/foo" {
		t.Errorf("source.path = %+v", d.Spec.Source)
	}
	if d.Spec.Source.Helm == nil || len(d.Spec.Source.Helm.ValueFiles) != 2 {
		t.Errorf("helm = %+v", d.Spec.Source.Helm)
	}
}

func TestWalkMultiDocAndList(t *testing.T) {
	root := t.TempDir()
	write(t, root, "bundle.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: one
spec:
  source:
    path: one
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: two
spec:
  source:
    path: two
`)
	write(t, root, "list.yaml", `
apiVersion: v1
kind: List
items:
  - apiVersion: argoproj.io/v1alpha1
    kind: Application
    metadata: {name: three}
    spec: {source: {path: three}}
  - apiVersion: v1
    kind: ConfigMap
    metadata: {name: ignore-me}
`)

	docs, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	names := make(map[string]bool)
	for _, d := range docs {
		names[d.Name] = true
	}
	for _, want := range []string{"one", "two", "three"} {
		if !names[want] {
			t.Errorf("missing %q in %v", want, names)
		}
	}
	if names["ignore-me"] {
		t.Errorf("ConfigMap should be filtered out")
	}
}

func TestWalkApplicationSetGenerators(t *testing.T) {
	root := t.TempDir()
	write(t, root, "appset.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata: {name: fleet}
spec:
  generators:
    - list:
        elements:
          - cluster: a
    - matrix:
        generators:
          - clusters: {}
          - git: {repoURL: https://example/r, directories: [{path: apps/*}]}
  template:
    metadata: {name: "{{cluster}}-app"}
    spec:
      source:
        path: charts/tpl
        helm:
          valueFiles: [values.yaml]
`)

	docs, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(docs) != 1 || docs[0].Kind != KindApplicationSet {
		t.Fatalf("got %+v", docs)
	}
	gens := docs[0].Spec.Generators
	for _, want := range []string{"list", "matrix", "clusters", "git"} {
		if !gens[want] {
			t.Errorf("missing generator kind %q in %v", want, gens)
		}
	}
	if docs[0].Spec.Template == nil || docs[0].Spec.Template.Spec.Source == nil {
		t.Fatalf("template not decoded: %+v", docs[0].Spec)
	}
	if docs[0].Spec.Template.Spec.Source.Path != "charts/tpl" {
		t.Errorf("template source path = %q", docs[0].Spec.Template.Spec.Source.Path)
	}
}

func TestWalkExcludeDir(t *testing.T) {
	root := t.TempDir()
	write(t, root, "keep/ok.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: keep}
spec: {source: {path: x}}
`)
	write(t, root, "skip/hide.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: skip}
spec: {source: {path: x}}
`)

	docs, err := Walk(root, Options{ExcludeDirs: []string{"skip"}})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(docs) != 1 || docs[0].Name != "keep" {
		t.Fatalf("exclude-dir failed: %+v", docs)
	}
}

func TestWalkIncludeRegex(t *testing.T) {
	root := t.TempDir()
	write(t, root, "envs/prod/app.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: prod}
spec: {source: {path: x}}
`)
	write(t, root, "envs/dev/app.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: dev}
spec: {source: {path: x}}
`)

	docs, err := Walk(root, Options{IncludeRegex: `envs/prod/`})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(docs) != 1 || docs[0].Name != "prod" {
		t.Fatalf("include-regex failed: %+v", docs)
	}
}

func TestWalkSkipsGitDir(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".git/HEAD", "ref: refs/heads/main\n")
	write(t, root, ".git/apps.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: hidden}
spec: {source: {path: x}}
`)

	docs, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf(".git should be skipped; got %+v", docs)
	}
}
