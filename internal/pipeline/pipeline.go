// Package pipeline orchestrates the full run: git resolution, discovery,
// changeset computation, cluster bring-up, rendering, diffing, and report
// posting.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/szhekpisov/preview-argocd-diff/internal/changeset"
	"github.com/szhekpisov/preview-argocd-diff/internal/cluster"
	"github.com/szhekpisov/preview-argocd-diff/internal/config"
	"github.com/szhekpisov/preview-argocd-diff/internal/differ"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
	gogit "github.com/szhekpisov/preview-argocd-diff/internal/git"
	"github.com/szhekpisov/preview-argocd-diff/internal/render"
	"github.com/szhekpisov/preview-argocd-diff/internal/report"
	"github.com/szhekpisov/preview-argocd-diff/internal/vcs"
)

// Deps bundles the overridable collaborators. Leaving a field nil requests
// the default production implementation.
type Deps struct {
	Runner       cluster.Runner
	DifferImpl   differ.Differ
	VCSImpl      vcs.VCS
	MakeCluster  func(cluster.Runner, cluster.KindOptions) ClusterManager
	MakeArgoCD   func(cluster.Runner, cluster.ArgoCDOptions) ArgoCDManager
	MakeRenderer func(opts render.ArgoCDOptions) render.Renderer
	Logger       *slog.Logger
}

// ClusterManager is what the pipeline needs from the Kind wrapper.
type ClusterManager interface {
	Ensure(ctx context.Context) error
	Teardown(ctx context.Context) error
	Kubeconfig(ctx context.Context) ([]byte, error)
}

// ArgoCDManager is what the pipeline needs from the Argo CD installer.
type ArgoCDManager interface {
	Install(ctx context.Context) error
	WaitForHealthy(ctx context.Context) error
}

// Run executes the pipeline against cfg. It is safe to call more than once
// with fresh config/deps.
func Run(ctx context.Context, cfg *config.Config, deps Deps) error {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: levelFrom(cfg.LogLevel)}))
	}

	if deps.Runner == nil {
		deps.Runner = cluster.NewExecRunner()
	}
	if deps.DifferImpl == nil {
		deps.DifferImpl = differ.NewBuiltin()
	}
	if deps.MakeCluster == nil {
		deps.MakeCluster = func(r cluster.Runner, o cluster.KindOptions) ClusterManager {
			return &cluster.Kind{Runner: r, Opts: o}
		}
	}
	if deps.MakeArgoCD == nil {
		deps.MakeArgoCD = func(r cluster.Runner, o cluster.ArgoCDOptions) ArgoCDManager {
			return &cluster.ArgoCD{Runner: r, Opts: o}
		}
	}
	if deps.MakeRenderer == nil {
		deps.MakeRenderer = func(opts render.ArgoCDOptions) render.Renderer {
			return render.NewArgoCD(opts)
		}
	}

	logger.Info("starting", "repo", cfg.Repo, "base", cfg.BaseBranch, "head", cfg.HeadBranch)

	// 1. Git: open, resolve refs, materialize both trees.
	repo, err := gogit.Open(cfg.RepoRoot)
	if err != nil {
		return err
	}
	baseHash, err := repo.ResolveRef(cfg.BaseBranch)
	if err != nil {
		return fmt.Errorf("resolve base branch %q: %w", cfg.BaseBranch, err)
	}
	headRef := cfg.HeadBranch
	if headRef == "" {
		headRef = "HEAD"
	}
	headHash, err := repo.ResolveRef(headRef)
	if err != nil {
		return fmt.Errorf("resolve head branch %q: %w", headRef, err)
	}

	workDir, err := os.MkdirTemp("", "padp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	baseDir := filepath.Join(workDir, "base")
	headDir := filepath.Join(workDir, "head")
	logger.Info("materializing trees", "base", baseHash.String(), "head", headHash.String(), "workDir", workDir)
	if err := repo.Materialize(baseHash, baseDir); err != nil {
		return fmt.Errorf("materialize base: %w", err)
	}
	if err := repo.Materialize(headHash, headDir); err != nil {
		return fmt.Errorf("materialize head: %w", err)
	}

	changedFiles, err := repo.ChangedFiles(baseHash, headHash)
	if err != nil {
		return fmt.Errorf("changed files: %w", err)
	}
	logger.Info("changed files", "count", len(changedFiles))

	// 2. Discover CRDs in both trees.
	opts := discover.Options{ExcludeDirs: cfg.ExcludeDirs, IncludeRegex: cfg.IncludeRegex}
	baseDocs, err := discover.Walk(baseDir, opts)
	if err != nil {
		logger.Warn("base-tree decode errors", "err", err)
	}
	headDocs, err := discover.Walk(headDir, opts)
	if err != nil {
		logger.Warn("head-tree decode errors", "err", err)
	}

	// 3. Changeset.
	var affected changeset.Affected
	if cfg.All {
		affected = allAsChanges(headDocs)
	} else {
		affected = changeset.Build(baseDocs, headDocs, changedFiles)
	}
	logger.Info("affected apps", "count", len(affected))
	if len(affected) > cfg.MaxApps {
		return fmt.Errorf("fan-out guard: %d apps affected exceeds --max-apps=%d; narrow with --selector or --include-regex", len(affected), cfg.MaxApps)
	}
	if len(affected) == 0 {
		logger.Info("no apps affected — skipping render")
		return writeAndMaybePost(ctx, cfg, deps, report.Build(report.Input{Title: cfg.MarkdownTitle}, 0))
	}

	// 4. Cluster bring-up.
	kind := deps.MakeCluster(deps.Runner, cluster.KindOptions{
		Name:          cfg.ClusterName,
		ReuseIfExists: cfg.ReuseCluster,
		KeepOnExit:    cfg.KeepCluster,
	})
	if err := kind.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure kind: %w", err)
	}
	defer func() {
		if err := kind.Teardown(ctx); err != nil {
			logger.Warn("teardown", "err", err)
		}
	}()

	kubeconfig, err := kind.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}
	kcPath := filepath.Join(workDir, "kubeconfig")
	if err := os.WriteFile(kcPath, kubeconfig, 0o600); err != nil {
		return err
	}

	argo := deps.MakeArgoCD(deps.Runner, cluster.ArgoCDOptions{
		Namespace:      cfg.ArgoCDNamespace,
		ChartVersion:   cfg.ArgoCDChartVersion,
		KubeconfigPath: kcPath,
	})
	if err := argo.Install(ctx); err != nil {
		return err
	}
	if err := argo.WaitForHealthy(ctx); err != nil {
		return err
	}

	// 5. Render + diff each affected app.
	renderer := deps.MakeRenderer(render.ArgoCDOptions{
		Runner:          deps.Runner,
		KubeconfigPath:  kcPath,
		ArgoCDNamespace: cfg.ArgoCDNamespace,
		RepoURL:         repoURLFromGitHub(cfg.Repo),
	})

	var reports []report.ChangeReport
	for _, c := range affected {
		cr := report.ChangeReport{Key: c.Key, Status: c.Status, Reasons: c.Reasons}

		if c.Head != nil {
			if ok, reason := renderer.Capable(*c.Head); !ok {
				cr.RenderErr = reason
				reports = append(reports, cr)
				continue
			}
		}

		var baseOut, headOut []byte
		if c.Base != nil {
			if b, err := renderer.Render(ctx, *c.Base, baseHash.String()); err != nil {
				cr.RenderErr = "render base: " + err.Error()
				reports = append(reports, cr)
				continue
			} else {
				baseOut = b
			}
		}
		if c.Head != nil {
			if h, err := renderer.Render(ctx, *c.Head, headHash.String()); err != nil {
				cr.RenderErr = "render head: " + err.Error()
				reports = append(reports, cr)
				continue
			} else {
				headOut = h
			}
		}

		d, err := deps.DifferImpl.Diff(ctx, c.Key.Name, baseOut, headOut)
		if err != nil {
			cr.RenderErr = "diff: " + err.Error()
		} else {
			cr.Diff = d
		}
		reports = append(reports, cr)
	}

	// 6. Report.
	body := report.Build(report.Input{
		Title:   cfg.MarkdownTitle,
		Changes: reports,
	}, 0)
	return writeAndMaybePost(ctx, cfg, deps, body)
}

func writeAndMaybePost(ctx context.Context, cfg *config.Config, deps Deps, body string) error {
	if cfg.OutputDir != "" {
		if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(cfg.OutputDir, "diff.md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if cfg.PR <= 0 {
		return nil
	}
	v := deps.VCSImpl
	if v == nil {
		v = vcs.NewGitHub(cfg.GitHubToken)
	}
	return v.PostOrUpdateComment(ctx, cfg.Repo, cfg.PR, report.Marker, body)
}

func allAsChanges(headDocs []discover.Doc) changeset.Affected {
	out := make(changeset.Affected, 0, len(headDocs))
	for i := range headDocs {
		d := &headDocs[i]
		out = append(out, changeset.Change{
			Key:     changeset.DocKey{Kind: d.Kind, Namespace: d.Namespace, Name: d.Name},
			Status:  changeset.StatusModified,
			Head:    d,
			Reasons: []string{"--all specified"},
		})
	}
	return out
}

func repoURLFromGitHub(ownerRepo string) string {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 {
		return ownerRepo
	}
	return "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
}

func levelFrom(name string) slog.Level {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ErrNothingToRender is returned by Run when no apps are affected; callers can
// choose to treat this as a successful no-op.
var ErrNothingToRender = errors.New("no affected apps")
