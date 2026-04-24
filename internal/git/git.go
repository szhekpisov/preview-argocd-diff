// Package git wraps go-git for the two operations this tool needs:
// resolving refs, listing files changed between two refs, and materializing a
// ref into a standalone working directory.
package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repo is a handle to an on-disk git repository.
type Repo struct {
	root string
	repo *git.Repository
}

// Open opens an existing git repository at root.
func Open(root string) (*Repo, error) {
	r, err := git.PlainOpen(root)
	if err != nil {
		return nil, fmt.Errorf("open git repo at %q: %w", root, err)
	}
	return &Repo{root: root, repo: r}, nil
}

// ResolveRef resolves a ref name (branch, tag, or SHA) to a commit hash.
// Branch names like "main" are tried as local branches then as remote
// refs/remotes/origin/<name>, matching the behavior most CI runners expect.
func (r *Repo) ResolveRef(name string) (plumbing.Hash, error) {
	if name == "" {
		return plumbing.ZeroHash, errors.New("empty ref name")
	}

	for _, candidate := range []plumbing.ReferenceName{
		plumbing.ReferenceName(name),
		plumbing.NewBranchReferenceName(name),
		plumbing.NewRemoteReferenceName("origin", name),
		plumbing.NewTagReferenceName(name),
	} {
		if ref, err := r.repo.Reference(candidate, true); err == nil {
			return ref.Hash(), nil
		}
	}

	// Fall back to resolving as a commit-ish (SHA or abbreviated SHA).
	if h, err := r.repo.ResolveRevision(plumbing.Revision(name)); err == nil {
		return *h, nil
	}

	return plumbing.ZeroHash, fmt.Errorf("unknown ref %q", name)
}

// ChangedFiles returns the set of files changed between base and head commits,
// as repo-root-relative forward-slashed paths. The result is sorted and
// deduplicated, covering Added, Modified, Deleted, and Renamed entries.
func (r *Repo) ChangedFiles(base, head plumbing.Hash) ([]string, error) {
	baseCommit, err := r.repo.CommitObject(base)
	if err != nil {
		return nil, fmt.Errorf("load base commit %s: %w", base, err)
	}
	headCommit, err := r.repo.CommitObject(head)
	if err != nil {
		return nil, fmt.Errorf("load head commit %s: %w", head, err)
	}

	patch, err := baseCommit.Patch(headCommit)
	if err != nil {
		return nil, fmt.Errorf("diff %s..%s: %w", base, head, err)
	}

	seen := make(map[string]struct{})
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		if from != nil {
			seen[from.Path()] = struct{}{}
		}
		if to != nil {
			seen[to.Path()] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Materialize writes the tree at ref into dst, an empty directory. It clones
// only the working-tree files — .git is not included — which is all the
// downstream renderers need.
func (r *Repo) Materialize(ref plumbing.Hash, dst string) error {
	commit, err := r.repo.CommitObject(ref)
	if err != nil {
		return fmt.Errorf("load commit %s: %w", ref, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("load tree for %s: %w", ref, err)
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	return tree.Files().ForEach(func(f *gitFile) error {
		target := filepath.Join(dst, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Reader()
		if err != nil {
			return fmt.Errorf("open %s: %w", f.Name, err)
		}
		defer rc.Close()

		mode, err := f.Mode.ToOSFileMode()
		if err != nil {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := copyReader(out, rc); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
