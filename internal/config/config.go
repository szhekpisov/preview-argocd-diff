// Package config loads the tool's runtime configuration from, in order of
// precedence: command-line flags, environment variables, optional YAML file,
// and built-in defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	envPrefix     = "PADP"
	defaultMaxApp = 50
)

// Config is the single source of truth for a run.
type Config struct {
	v *viper.Viper

	// Repo identity and refs.
	Repo       string
	BaseBranch string
	HeadBranch string
	PR         int

	// Discovery.
	RepoRoot     string
	ExcludeDirs  []string
	IncludeRegex string
	Selector     string
	MaxApps      int
	All          bool

	// Cluster + ArgoCD.
	KindVersion        string
	ArgoCDVersion      string
	ArgoCDChartVersion string
	ArgoCDNamespace    string
	ReuseCluster       bool
	KeepCluster        bool
	ClusterName        string
	ClusterMode        bool

	// Diff.
	DiffTool        string
	DiffArgs        string
	DiffIgnore      string
	IgnoreResources string

	// Output.
	OutputDir     string
	MarkdownTitle string

	// Auth.
	GitHubToken string

	// Misc.
	ConfigFile string
	LogLevel   string
}

// New builds a Config seeded with a fresh Viper.
func New() *Config {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	return &Config{v: v}
}

// BindFlags registers every CLI flag onto the given flag set and binds each to
// its matching env/config key. Call once during command construction.
func (c *Config) BindFlags(fs *pflag.FlagSet) {
	fs.String("repo", "", "GitHub repository as owner/name (auto-detected from $GITHUB_REPOSITORY)")
	fs.String("base-branch", "main", "Base branch to diff against")
	fs.String("head-branch", "", "Head branch (defaults to current HEAD)")
	fs.Int("pr", 0, "Pull-request number (auto-detected from $GITHUB_REF where possible)")

	fs.String("repo-root", ".", "Path to the local checkout of the repository")
	fs.StringSlice("exclude-dir", nil, "Directories to exclude from YAML discovery (repeatable)")
	fs.String("include-regex", "", "Regex for file paths to include in discovery")
	fs.String("selector", "", "Label selector applied to Application CRDs")
	fs.Int("max-apps", defaultMaxApp, "Fan-out guard: fail if more than N apps would be rendered")
	fs.Bool("all", false, "Render every discovered app (bypass only-changed mode)")

	fs.String("kind-version", "", "Pin kind CLI version")
	fs.String("argocd-version", "", "Pin argocd CLI version")
	fs.String("argocd-chart-version", "", "Pin ArgoCD Helm chart version")
	fs.String("argocd-namespace", "argocd", "Namespace ArgoCD is installed into")
	fs.Bool("reuse-cluster", false, "Reuse an existing Kind cluster if present")
	fs.Bool("keep-cluster", false, "Skip teardown of the Kind cluster after the run")
	fs.String("cluster-name", "padp", "Kind cluster name")
	fs.Bool("cluster-mode", false, "Use Kind + real ArgoCD instead of offline 'helm template' rendering")

	fs.String("diff-tool", "builtin", "Diff tool: builtin, a command in $PATH, or an absolute path")
	fs.String("diff-args", "", "Template passed to the external diff tool with {base} and {head} placeholders")
	fs.String("diff-ignore", "", "Regex of diff lines to strip from output")
	fs.String("ignore-resources", "", "Regex of group:kind:name triples to skip")

	fs.String("output-dir", "./output", "Directory for markdown report and per-app manifests")
	fs.String("markdown-title", "ArgoCD Diff Preview", "Title heading for the PR comment")

	fs.String("github-token", "", "GitHub token (falls back to $GITHUB_TOKEN)")
	fs.String("config", "", "Optional YAML config file")
	fs.String("log-level", "info", "Log level: debug, info, warn, error")

	_ = c.v.BindPFlags(fs)
}

// Load finalizes configuration: parses the optional YAML file, then populates
// the Config struct fields from Viper. Call after Cobra has parsed flags.
func (c *Config) Load(cmd *cobra.Command) error {
	if path, _ := cmd.Flags().GetString("config"); path != "" {
		c.v.SetConfigFile(path)
		if err := c.v.ReadInConfig(); err != nil {
			return fmt.Errorf("read config file %q: %w", path, err)
		}
	}

	c.Repo = c.v.GetString("repo")
	c.BaseBranch = c.v.GetString("base-branch")
	c.HeadBranch = c.v.GetString("head-branch")
	c.PR = c.v.GetInt("pr")

	c.RepoRoot = c.v.GetString("repo-root")
	c.ExcludeDirs = c.v.GetStringSlice("exclude-dir")
	c.IncludeRegex = c.v.GetString("include-regex")
	c.Selector = c.v.GetString("selector")
	c.MaxApps = c.v.GetInt("max-apps")
	c.All = c.v.GetBool("all")

	c.KindVersion = c.v.GetString("kind-version")
	c.ArgoCDVersion = c.v.GetString("argocd-version")
	c.ArgoCDChartVersion = c.v.GetString("argocd-chart-version")
	c.ArgoCDNamespace = c.v.GetString("argocd-namespace")
	c.ReuseCluster = c.v.GetBool("reuse-cluster")
	c.KeepCluster = c.v.GetBool("keep-cluster")
	c.ClusterName = c.v.GetString("cluster-name")
	c.ClusterMode = c.v.GetBool("cluster-mode")

	c.DiffTool = c.v.GetString("diff-tool")
	c.DiffArgs = c.v.GetString("diff-args")
	c.DiffIgnore = c.v.GetString("diff-ignore")
	c.IgnoreResources = c.v.GetString("ignore-resources")

	c.OutputDir = c.v.GetString("output-dir")
	c.MarkdownTitle = c.v.GetString("markdown-title")

	c.GitHubToken = c.v.GetString("github-token")
	if c.GitHubToken == "" {
		c.GitHubToken = os.Getenv("GITHUB_TOKEN")
	}

	c.ConfigFile = c.v.GetString("config")
	c.LogLevel = c.v.GetString("log-level")

	if c.Repo == "" {
		c.Repo = os.Getenv("GITHUB_REPOSITORY")
	}
	return nil
}

// Validate checks that required fields are set and values are consistent.
func (c *Config) Validate() error {
	var errs []string

	if c.Repo == "" {
		errs = append(errs, "--repo is required (or set $GITHUB_REPOSITORY)")
	} else if !strings.Contains(c.Repo, "/") {
		errs = append(errs, "--repo must be owner/name")
	}
	if c.BaseBranch == "" {
		errs = append(errs, "--base-branch is required")
	}
	if c.MaxApps <= 0 {
		errs = append(errs, "--max-apps must be positive")
	}
	if c.DiffTool != "builtin" && c.DiffArgs == "" {
		errs = append(errs, "--diff-args is required when --diff-tool is not 'builtin'")
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New("invalid config: " + strings.Join(errs, "; "))
}

// Summary returns a short string describing the effective config, useful for
// debug logging at the top of a run.
func (c *Config) Summary() string {
	return fmt.Sprintf("repo=%s base=%s head=%s pr=%d cluster=%s mode=%s",
		c.Repo, c.BaseBranch, c.HeadBranch, c.PR, c.ClusterName, renderMode(c))
}

func renderMode(c *Config) string {
	if c.All {
		return "all"
	}
	return "only-changed"
}
