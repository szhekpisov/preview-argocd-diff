package cluster

import (
	"context"
	"fmt"
)

// ArgoCDOptions configures the Helm install of Argo CD into the cluster.
type ArgoCDOptions struct {
	Namespace    string
	ChartVersion string
	ChartRepo    string // default: https://argoproj.github.io/argo-helm
	ReleaseName  string // default: argocd
	// KubeconfigPath is passed to helm and kubectl invocations.
	KubeconfigPath string
}

// ArgoCD installs and manages Argo CD inside an already-bootstrapped cluster.
type ArgoCD struct {
	Runner Runner
	Opts   ArgoCDOptions
}

// Install runs `helm repo add`, `helm repo update`, then `helm upgrade
// --install argocd argo/argo-cd --namespace <ns> --create-namespace --wait`.
func (a *ArgoCD) Install(ctx context.Context) error {
	if a.Opts.Namespace == "" {
		a.Opts.Namespace = "argocd"
	}
	if a.Opts.ReleaseName == "" {
		a.Opts.ReleaseName = "argocd"
	}
	if a.Opts.ChartRepo == "" {
		a.Opts.ChartRepo = "https://argoproj.github.io/argo-helm"
	}

	env := []string{}
	if a.Opts.KubeconfigPath != "" {
		env = append(env, "KUBECONFIG="+a.Opts.KubeconfigPath)
	}

	steps := [][]string{
		{"helm", "repo", "add", "argo", a.Opts.ChartRepo},
		{"helm", "repo", "update", "argo"},
	}
	for _, s := range steps {
		if _, err := a.Runner.Run(ctx, Command{Name: s[0], Args: s[1:], Env: env}); err != nil {
			return fmt.Errorf("argocd install: %w", err)
		}
	}

	args := []string{
		"upgrade", "--install", a.Opts.ReleaseName, "argo/argo-cd",
		"--namespace", a.Opts.Namespace,
		"--create-namespace",
		"--wait",
	}
	if a.Opts.ChartVersion != "" {
		args = append(args, "--version", a.Opts.ChartVersion)
	}
	_, err := a.Runner.Run(ctx, Command{Name: "helm", Args: args, Env: env})
	if err != nil {
		return fmt.Errorf("helm install argocd: %w", err)
	}
	return nil
}

// WaitForHealthy waits for the argocd-server and argocd-repo-server
// deployments to roll out.
func (a *ArgoCD) WaitForHealthy(ctx context.Context) error {
	env := []string{}
	if a.Opts.KubeconfigPath != "" {
		env = append(env, "KUBECONFIG="+a.Opts.KubeconfigPath)
	}
	for _, dep := range []string{"argocd-server", "argocd-repo-server", "argocd-application-controller"} {
		args := []string{"-n", a.Opts.Namespace, "rollout", "status", "deployment/" + dep, "--timeout=5m"}
		if _, err := a.Runner.Run(ctx, Command{Name: "kubectl", Args: args, Env: env}); err != nil {
			// argocd-application-controller is a StatefulSet in recent charts;
			// try that if the deployment form fails.
			args = []string{"-n", a.Opts.Namespace, "rollout", "status", "statefulset/" + dep, "--timeout=5m"}
			if _, err2 := a.Runner.Run(ctx, Command{Name: "kubectl", Args: args, Env: env}); err2 != nil {
				return fmt.Errorf("wait for %s: %w", dep, err)
			}
		}
	}
	return nil
}
