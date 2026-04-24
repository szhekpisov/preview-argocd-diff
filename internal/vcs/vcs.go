// Package vcs abstracts over the PR-commenting backend. The first
// implementation targets GitHub; GitLab / Bitbucket are planned follow-ups.
package vcs

import "context"

// VCS posts (or edits in place) a single PR comment.
type VCS interface {
	// PostOrUpdateComment searches the PR's existing comments for one
	// containing marker and updates it; if none is found, a new comment is
	// created. Implementations should treat repeated calls with the same
	// (repo, pr, marker) as idempotent updates.
	PostOrUpdateComment(ctx context.Context, repo string, pr int, marker, body string) error
}
