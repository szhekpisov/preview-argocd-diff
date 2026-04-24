package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/config"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
	"github.com/szhekpisov/preview-argocd-diff/internal/render"
	"github.com/szhekpisov/preview-argocd-diff/internal/report"
)

// fakeRunner records every invocation; no command fails.
type fakeRunner struct{ calls []cluster.Command }

func (f *fakeRunner) Run(_ context.Context, c cluster.Command) (cluster.Result, error) {
	f.calls = append(f.calls, c)
	return cluster.Result{Stdout: "padp-cluster\n"}, nil
}

type noopCluster struct{}

func (noopCluster) Ensure(context.Context) error   { return nil }
func (noopCluster) Teardown(context.Context) error { return nil }
func (noopCluster) Kubeconfig(context.Context) ([]byte, error) {
	return []byte("apiVersion: v1\nkind: Config"), nil
}

type noopArgoCD struct{}

func (noopArgoCD) Install(context.Context) error        { return nil }
func (noopArgoCD) WaitForHealthy(context.Context) error { return nil }

// stubRenderer returns deterministic output keyed by (name, ref) so the diff
// in the test is non-empty.
type stubRenderer struct{}

func (stubRenderer) Capable(discover.Doc) (bool, string) { return true, "" }
func (stubRenderer) Render(_ context.Context, app discover.Doc, ref string) ([]byte, error) {
	return []byte("app: " + app.Name + "\nref: " + ref + "\n"), nil
}

// fakeVCS records the last comment for assertions.
type fakeVCS struct {
	repo, marker, body string
	pr                 int
}

func (f *fakeVCS) PostOrUpdateComment(_ context.Context, repo string, pr int, marker, body string) error {
	f.repo, f.pr, f.marker, f.body = repo, pr, marker, body
	return nil
}

// makeRepo builds a git repo with one commit on main; if withAppChange, a
// "feature" branch adds an ArgoCD Application whose source path matches a
// changed file, so the changeset engine marks it affected.
func makeRepo(t *testing.T, withAppChange bool) (root string, baseBranch, headBranch string) {
	t.Helper()
	root = t.TempDir()
	r, err := gogit.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}

	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(rel); err != nil {
			t.Fatal(err)
		}
	}

	// Main: one Application whose source is charts/foo.
	write("apps/foo.yaml", `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: foo}
spec:
  source:
    repoURL: https://example.com/r
    path: charts/foo
    targetRevision: HEAD
`)
	write("charts/foo/values.yaml", "image: one\n")
	if _, err := wt.Commit("init", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("feature"), Create: true}); err != nil {
		t.Fatal(err)
	}
	if withAppChange {
		write("charts/foo/values.yaml", "image: two\n")
	} else {
		write("unrelated.md", "just docs\n")
	}
	if _, err := wt.Commit("change", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}

	return root, "main", "feature"
}

func buildCfg(root, base, head string, pr int) *config.Config {
	c := config.New()
	c.Repo = "acme/widgets"
	c.RepoRoot = root
	c.BaseBranch = base
	c.HeadBranch = head
	c.PR = pr
	c.MaxApps = 50
	c.ClusterName = "padp"
	c.ArgoCDNamespace = "argocd"
	c.DiffTool = "builtin"
	c.MarkdownTitle = "Test"
	c.OutputDir = ""
	c.LogLevel = "error"
	return c
}

func pipelineDeps(vcsMock *fakeVCS) Deps {
	return Deps{
		Runner:  &fakeRunner{},
		VCSImpl: vcsMock,
		MakeCluster: func(cluster.Runner, cluster.KindOptions) ClusterManager {
			return noopCluster{}
		},
		MakeArgoCD: func(cluster.Runner, cluster.ArgoCDOptions) ArgoCDManager {
			return noopArgoCD{}
		},
		MakeRenderer: func(render.ArgoCDOptions) render.Renderer { return stubRenderer{} },
	}
}

func TestPipelineNoChanges(t *testing.T) {
	root, base, head := makeRepo(t, false)
	vcsMock := &fakeVCS{}
	cfg := buildCfg(root, base, head, 42)

	if err := Run(context.Background(), cfg, pipelineDeps(vcsMock)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if vcsMock.pr != 42 {
		t.Errorf("VCS not called with PR 42: %+v", vcsMock)
	}
	if !strings.Contains(vcsMock.body, report.Marker) {
		t.Errorf("body missing marker")
	}
	if !strings.Contains(vcsMock.body, "0 changed · 0 added · 0 removed") {
		t.Errorf("unexpected body: %s", vcsMock.body)
	}
}

func TestPipelineWithChange(t *testing.T) {
	root, base, head := makeRepo(t, true)
	vcsMock := &fakeVCS{}
	cfg := buildCfg(root, base, head, 42)

	if err := Run(context.Background(), cfg, pipelineDeps(vcsMock)); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(vcsMock.body, "1 changed") {
		t.Errorf("expected 1 change, body:\n%s", vcsMock.body)
	}
	if !strings.Contains(vcsMock.body, "foo") {
		t.Errorf("expected app name foo in body, got:\n%s", vcsMock.body)
	}
	// The stub renderer returns different content per ref, so diff is non-empty.
	if !strings.Contains(vcsMock.body, "```diff") {
		t.Errorf("expected diff block, got:\n%s", vcsMock.body)
	}
}

func TestPipelineFanOutGuard(t *testing.T) {
	root, base, head := makeRepo(t, true)
	cfg := buildCfg(root, base, head, 0)
	cfg.MaxApps = 0 // force guard to trip

	err := Run(context.Background(), cfg, pipelineDeps(&fakeVCS{}))
	if err == nil || !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected fan-out error, got %v", err)
	}
}
