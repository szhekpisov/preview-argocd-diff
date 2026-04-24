package cluster

import (
	"context"
	"fmt"
	"strings"
)

// KindOptions configures the Kind cluster.
type KindOptions struct {
	Name          string // cluster name, passed to `kind --name`
	NodeImage     string // optional; e.g. kindest/node:v1.29.2
	ReuseIfExists bool
	KeepOnExit    bool
}

// Kind is a Manager that drives the `kind` CLI.
type Kind struct {
	Runner Runner
	Opts   KindOptions
}

// Exists reports whether a cluster with the configured name is already up.
func (k *Kind) Exists(ctx context.Context) (bool, error) {
	res, err := k.Runner.Run(ctx, Command{Name: "kind", Args: []string{"get", "clusters"}})
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if strings.TrimSpace(line) == k.Opts.Name {
			return true, nil
		}
	}
	return false, nil
}

// Ensure brings the cluster up, reusing an existing one when ReuseIfExists is
// set. No-op if it's already there and reuse is allowed.
func (k *Kind) Ensure(ctx context.Context) error {
	if k.Opts.Name == "" {
		return fmt.Errorf("kind: cluster name is required")
	}
	exists, err := k.Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		if k.Opts.ReuseIfExists {
			return nil
		}
		return fmt.Errorf("kind cluster %q already exists; pass --reuse-cluster or pick a different --cluster-name", k.Opts.Name)
	}

	args := []string{"create", "cluster", "--name", k.Opts.Name}
	if k.Opts.NodeImage != "" {
		args = append(args, "--image", k.Opts.NodeImage)
	}
	_, err = k.Runner.Run(ctx, Command{Name: "kind", Args: args})
	return err
}

// Teardown deletes the cluster, or no-ops if KeepOnExit is set.
func (k *Kind) Teardown(ctx context.Context) error {
	if k.Opts.KeepOnExit {
		return nil
	}
	_, err := k.Runner.Run(ctx, Command{
		Name: "kind",
		Args: []string{"delete", "cluster", "--name", k.Opts.Name},
	})
	return err
}

// Kubeconfig returns the kubeconfig for this cluster as a byte slice. The
// caller typically writes it to a temp file and exports KUBECONFIG.
func (k *Kind) Kubeconfig(ctx context.Context) ([]byte, error) {
	res, err := k.Runner.Run(ctx, Command{
		Name: "kind",
		Args: []string{"get", "kubeconfig", "--name", k.Opts.Name},
	})
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
