package git

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// setupRepo initializes a temporary git repo with two commits on two branches:
//
//	main:    file.txt("one\n"), shared.txt("common\n")
//	feature: main + file.txt("two\n") + added.txt("new\n"); shared.txt deleted
//
// It returns the repo handle and the commit hashes (base=main, head=feature).
func setupRepo(t *testing.T) (*Repo, plumbing.Hash, plumbing.Hash) {
	t.Helper()
	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	// PlainInit defaults HEAD to refs/heads/master; point it at main so the
	// initial commit lands on the branch name used in the rest of the code.
	if err := repo.Storer.SetReference(
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")),
	); err != nil {
		t.Fatalf("retarget HEAD: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}

	write := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatal(err)
		}
	}

	// First commit on main.
	write("file.txt", "one\n")
	write("shared.txt", "common\n")
	baseHash, err := wt.Commit("initial", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit main: %v", err)
	}

	// Branch + second commit.
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature"),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout feature: %v", err)
	}
	write("file.txt", "two\n")
	write("added.txt", "new\n")
	if err := os.Remove(filepath.Join(dir, "shared.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("shared.txt"); err != nil {
		t.Fatal(err)
	}
	headHash, err := wt.Commit("feature-edit", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit feature: %v", err)
	}

	r, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return r, baseHash, headHash
}

func TestResolveRefBranch(t *testing.T) {
	r, base, head := setupRepo(t)

	got, err := r.ResolveRef("main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	if got != base {
		t.Errorf("main = %s, want %s", got, base)
	}

	got, err = r.ResolveRef("feature")
	if err != nil {
		t.Fatalf("resolve feature: %v", err)
	}
	if got != head {
		t.Errorf("feature = %s, want %s", got, head)
	}
}

func TestResolveRefSHA(t *testing.T) {
	r, base, _ := setupRepo(t)
	got, err := r.ResolveRef(base.String())
	if err != nil {
		t.Fatalf("resolve SHA: %v", err)
	}
	if got != base {
		t.Errorf("got %s want %s", got, base)
	}
}

func TestResolveRefUnknown(t *testing.T) {
	r, _, _ := setupRepo(t)
	if _, err := r.ResolveRef("does-not-exist"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChangedFiles(t *testing.T) {
	r, base, head := setupRepo(t)

	files, err := r.ChangedFiles(base, head)
	if err != nil {
		t.Fatalf("changed files: %v", err)
	}

	want := []string{"added.txt", "file.txt", "shared.txt"}
	sort.Strings(files)
	if len(files) != len(want) {
		t.Fatalf("got %v, want %v", files, want)
	}
	for i, w := range want {
		if files[i] != w {
			t.Errorf("[%d] = %q, want %q", i, files[i], w)
		}
	}
}

func TestMaterialize(t *testing.T) {
	r, base, head := setupRepo(t)

	baseDir := t.TempDir()
	if err := r.Materialize(base, baseDir); err != nil {
		t.Fatalf("materialize base: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(baseDir, "file.txt")); err != nil || string(body) != "one\n" {
		t.Errorf("base file.txt = %q err=%v, want %q", body, err, "one\n")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "added.txt")); !os.IsNotExist(err) {
		t.Errorf("base should not contain added.txt, stat err=%v", err)
	}

	headDir := t.TempDir()
	if err := r.Materialize(head, headDir); err != nil {
		t.Fatalf("materialize head: %v", err)
	}
	if body, _ := os.ReadFile(filepath.Join(headDir, "file.txt")); string(body) != "two\n" {
		t.Errorf("head file.txt = %q, want %q", body, "two\n")
	}
	if body, _ := os.ReadFile(filepath.Join(headDir, "added.txt")); string(body) != "new\n" {
		t.Errorf("head added.txt = %q, want %q", body, "new\n")
	}
	if _, err := os.Stat(filepath.Join(headDir, "shared.txt")); !os.IsNotExist(err) {
		t.Errorf("head should not contain shared.txt, stat err=%v", err)
	}
}
