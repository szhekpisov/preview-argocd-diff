package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records every Command and returns canned Results by the first
// token of the command + args.
type fakeRunner struct {
	calls []Command
	byKey map[string]Result
	errOn map[string]error
}

func (f *fakeRunner) Run(_ context.Context, c Command) (Result, error) {
	f.calls = append(f.calls, c)
	key := c.Name + " " + strings.Join(c.Args, " ")
	if err, ok := f.errOn[key]; ok {
		return Result{}, err
	}
	if r, ok := f.byKey[key]; ok {
		return r, nil
	}
	return Result{}, nil
}

func TestKindExistsFalse(t *testing.T) {
	r := &fakeRunner{byKey: map[string]Result{
		"kind get clusters": {Stdout: "other\n"},
	}}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp"}}
	exists, err := k.Exists(context.Background())
	if err != nil || exists {
		t.Fatalf("exists = %v err = %v", exists, err)
	}
}

func TestKindExistsTrue(t *testing.T) {
	r := &fakeRunner{byKey: map[string]Result{
		"kind get clusters": {Stdout: "padp\nanother\n"},
	}}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp"}}
	exists, err := k.Exists(context.Background())
	if err != nil || !exists {
		t.Fatalf("exists = %v err = %v", exists, err)
	}
}

func TestKindEnsureCreates(t *testing.T) {
	r := &fakeRunner{}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp", NodeImage: "kindest/node:v1.29.2"}}
	if err := k.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	// First call lists, second creates.
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(r.calls), r.calls)
	}
	create := r.calls[1]
	got := strings.Join(append([]string{create.Name}, create.Args...), " ")
	if !strings.Contains(got, "create cluster --name padp") {
		t.Errorf("create args = %q", got)
	}
	if !strings.Contains(got, "kindest/node:v1.29.2") {
		t.Errorf("image missing from create args: %q", got)
	}
}

func TestKindEnsureRejectsExistingWithoutReuse(t *testing.T) {
	r := &fakeRunner{byKey: map[string]Result{"kind get clusters": {Stdout: "padp\n"}}}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp"}}
	err := k.Ensure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists, got %v", err)
	}
}

func TestKindEnsureReuses(t *testing.T) {
	r := &fakeRunner{byKey: map[string]Result{"kind get clusters": {Stdout: "padp\n"}}}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp", ReuseIfExists: true}}
	if err := k.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 {
		t.Errorf("expected only list call, got %+v", r.calls)
	}
}

func TestKindTeardownKeep(t *testing.T) {
	r := &fakeRunner{}
	k := &Kind{Runner: r, Opts: KindOptions{Name: "padp", KeepOnExit: true}}
	if err := k.Teardown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Errorf("keep should be no-op, got %+v", r.calls)
	}
}

func TestArgoCDInstall(t *testing.T) {
	r := &fakeRunner{}
	a := &ArgoCD{Runner: r, Opts: ArgoCDOptions{ChartVersion: "7.6.0", KubeconfigPath: "/tmp/kc"}}
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected 3 helm calls, got %d", len(r.calls))
	}
	for _, c := range r.calls {
		found := false
		for _, e := range c.Env {
			if e == "KUBECONFIG=/tmp/kc" {
				found = true
			}
		}
		if !found {
			t.Errorf("KUBECONFIG missing from env: %+v", c.Env)
		}
	}
	last := r.calls[2]
	got := strings.Join(last.Args, " ")
	if !strings.Contains(got, "--version 7.6.0") {
		t.Errorf("chart version missing from upgrade args: %q", got)
	}
	if !strings.Contains(got, "--namespace argocd") {
		t.Errorf("default namespace wrong: %q", got)
	}
}

func TestArgoCDInstallSurfacesError(t *testing.T) {
	r := &fakeRunner{errOn: map[string]error{
		"helm repo update argo": errors.New("network down"),
	}}
	a := &ArgoCD{Runner: r}
	err := a.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}
