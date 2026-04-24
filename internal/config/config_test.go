package config

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// buildCmd returns a fresh Cobra command with Config bound, mirroring the real
// wiring in cmd/preview-argocd-diff/run.go.
func buildCmd(t *testing.T) (*cobra.Command, *Config) {
	t.Helper()
	cfg := New()
	cmd := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	cfg.BindFlags(cmd.Flags())
	return cmd, cfg
}

func TestDefaults(t *testing.T) {
	cmd, cfg := buildCmd(t)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := cfg.Load(cmd); err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.BaseBranch != "main" {
		t.Errorf("base-branch default = %q, want main", cfg.BaseBranch)
	}
	if cfg.MaxApps != 50 {
		t.Errorf("max-apps default = %d, want 50", cfg.MaxApps)
	}
	if cfg.DiffTool != "builtin" {
		t.Errorf("diff-tool default = %q, want builtin", cfg.DiffTool)
	}
	if cfg.ArgoCDNamespace != "argocd" {
		t.Errorf("argocd-namespace default = %q, want argocd", cfg.ArgoCDNamespace)
	}
}

func TestFlagsOverrideEnv(t *testing.T) {
	t.Setenv("PADP_REPO", "env-owner/env-repo")
	t.Setenv("PADP_BASE_BRANCH", "env-base")

	cmd, cfg := buildCmd(t)
	cmd.SetArgs([]string{"--repo", "flag-owner/flag-repo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := cfg.Load(cmd); err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Repo != "flag-owner/flag-repo" {
		t.Errorf("flag precedence failed: repo = %q", cfg.Repo)
	}
	// Env wins when no flag is set.
	if cfg.BaseBranch != "env-base" {
		t.Errorf("env precedence failed: base-branch = %q", cfg.BaseBranch)
	}
}

func TestValidateRejectsBadRepo(t *testing.T) {
	cmd, cfg := buildCmd(t)
	cmd.SetArgs([]string{"--repo", "no-slash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := cfg.Load(cmd); err != nil {
		t.Fatalf("load: %v", err)
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Fatalf("expected owner/name validation error, got %v", err)
	}
}

func TestValidateRequiresDiffArgsForExternalTool(t *testing.T) {
	cmd, cfg := buildCmd(t)
	cmd.SetArgs([]string{
		"--repo", "a/b",
		"--diff-tool", "delta",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := cfg.Load(cmd); err != nil {
		t.Fatalf("load: %v", err)
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "--diff-args is required") {
		t.Fatalf("expected diff-args validation error, got %v", err)
	}
}
