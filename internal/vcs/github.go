package vcs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v66/github"
)

// GitHub is a VCS implementation backed by google/go-github.
type GitHub struct {
	client *github.Client
}

// NewGitHub returns a GitHub VCS. Token is a PAT or GitHub Actions
// $GITHUB_TOKEN; an empty string falls back to unauthenticated access
// (sufficient only for public repos, which won't work for commenting).
func NewGitHub(token string) *GitHub {
	var c *github.Client
	if token == "" {
		c = github.NewClient(nil)
	} else {
		c = github.NewClient(nil).WithAuthToken(token)
	}
	return &GitHub{client: c}
}

// NewGitHubWithClient injects a pre-built github.Client; useful for tests
// pointing at an httptest server.
func NewGitHubWithClient(c *github.Client) *GitHub { return &GitHub{client: c} }

// PostOrUpdateComment implements VCS. It lists all issue comments on the PR
// (GitHub treats PRs as issues for comments), finds the first whose body
// contains marker, and either updates or creates as appropriate.
func (g *GitHub) PostOrUpdateComment(ctx context.Context, repo string, pr int, marker, body string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if pr <= 0 {
		return errors.New("vcs.github: pr number must be > 0")
	}
	if !strings.Contains(body, marker) {
		return errors.New("vcs.github: body must contain the marker for idempotent updates")
	}

	existing, err := g.findByMarker(ctx, owner, name, pr, marker)
	if err != nil {
		return err
	}
	if existing != nil {
		_, _, err := g.client.Issues.EditComment(ctx, owner, name, existing.GetID(), &github.IssueComment{Body: github.String(body)})
		return err
	}
	_, _, err = g.client.Issues.CreateComment(ctx, owner, name, pr, &github.IssueComment{Body: github.String(body)})
	return err
}

func (g *GitHub) findByMarker(ctx context.Context, owner, name string, pr int, marker string) (*github.IssueComment, error) {
	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		comments, resp, err := g.client.Issues.ListComments(ctx, owner, name, pr, opts)
		if err != nil {
			return nil, err
		}
		for _, c := range comments {
			if strings.Contains(c.GetBody(), marker) {
				return c, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return nil, nil
		}
		opts.Page = resp.NextPage
	}
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("vcs.github: repo must be owner/name, got %q", repo)
	}
	return parts[0], parts[1], nil
}

// Statically confirm GitHub implements VCS.
var _ VCS = (*GitHub)(nil)

// ensure we reference http so goimports doesn't drop it when testing helpers
// shift around.
var _ = http.StatusOK
