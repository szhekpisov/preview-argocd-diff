// Package report renders the PR-comment body from the set of affected apps
// and their rendered diffs.
//
// The comment starts with an HTML marker the VCS layer searches for when
// updating a previous comment in place. If the total body would exceed the
// configured byte cap (default: GitHub's 65,536 char limit), per-app diffs
// are truncated in order of lowest priority, and the footer points at the
// full report on disk.
package report

import (
	"fmt"
	"strings"

	"github.com/szhekpisov/preview-argocd-diff/internal/changeset"
)

// Marker is the invisible sentinel used to locate previous comments.
const Marker = "<!-- preview-argocd-diff:marker -->"

// DefaultMaxBytes is GitHub's hard limit for a single issue comment.
const DefaultMaxBytes = 65_000

// ChangeReport is the per-app input to the builder.
type ChangeReport struct {
	Key       changeset.DocKey
	Status    changeset.Status
	Reasons   []string
	Diff      string
	RenderErr string
}

// Input bundles everything the builder needs.
type Input struct {
	Title       string
	Changes     []ChangeReport
	ArtifactURL string // optional; added to the truncation footer when set
}

// Build renders the markdown body. maxBytes <= 0 uses DefaultMaxBytes.
func Build(in Input, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if in.Title == "" {
		in.Title = "ArgoCD Diff Preview"
	}

	errored, added, removed, modified := partition(in.Changes)
	summary := fmt.Sprintf("**Summary**: %d changed · %d added · %d removed · %d render errors",
		len(modified), len(added), len(removed), len(errored))

	header := strings.Join([]string{
		Marker,
		"## " + in.Title,
		"",
		summary,
		"",
	}, "\n")

	// Render sections in priority order. If we exceed the cap, earlier
	// sections are kept intact and later per-app diffs are truncated.
	//
	// Priority: errors > added > removed > modified.
	sections := []section{
		{title: "### Render errors", changes: errored, renderDiff: false},
		{title: "### Added Applications", changes: added, renderDiff: true},
		{title: "### Removed Applications", changes: removed, renderDiff: true},
		{title: "### Changed Applications", changes: modified, renderDiff: true},
	}

	body := header
	truncated := false
	for _, s := range sections {
		if len(s.changes) == 0 {
			continue
		}
		body += s.title + "\n\n"
		for _, c := range s.changes {
			block := renderChange(c, s.renderDiff)
			if len(body)+len(block) > maxBytes {
				truncated = true
				body += truncationNotice(c.Key.String())
				break
			}
			body += block
		}
		if truncated {
			break
		}
	}

	if truncated {
		body += "\n_Full report truncated to fit GitHub's comment size limit._"
		if in.ArtifactURL != "" {
			body += fmt.Sprintf(" See full report: %s\n", in.ArtifactURL)
		} else {
			body += "\n"
		}
	}
	return body
}

type section struct {
	title      string
	changes    []ChangeReport
	renderDiff bool
}

func partition(cs []ChangeReport) (errored, added, removed, modified []ChangeReport) {
	for _, c := range cs {
		switch {
		case c.RenderErr != "":
			errored = append(errored, c)
		case c.Status == changeset.StatusAdded:
			added = append(added, c)
		case c.Status == changeset.StatusRemoved:
			removed = append(removed, c)
		default:
			modified = append(modified, c)
		}
	}
	return
}

func renderChange(c ChangeReport, withDiff bool) string {
	var sb strings.Builder
	summary := c.Key.String()
	if len(c.Reasons) > 0 {
		summary += " — " + c.Reasons[0]
	}
	sb.WriteString("<details><summary>")
	sb.WriteString(escapeSummary(summary))
	sb.WriteString("</summary>\n\n")

	if len(c.Reasons) > 1 {
		sb.WriteString("Reasons:\n")
		for _, r := range c.Reasons {
			sb.WriteString("- " + r + "\n")
		}
		sb.WriteString("\n")
	}

	if c.RenderErr != "" {
		sb.WriteString("```\nrender error: " + c.RenderErr + "\n```\n")
	} else if withDiff && c.Diff != "" {
		sb.WriteString("```diff\n")
		sb.WriteString(c.Diff)
		if !strings.HasSuffix(c.Diff, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
	}
	sb.WriteString("</details>\n\n")
	return sb.String()
}

func truncationNotice(stoppedAt string) string {
	return fmt.Sprintf("\n_(truncated before `%s`)_\n", stoppedAt)
}

// escapeSummary prevents markdown injection through app names.
func escapeSummary(s string) string {
	return strings.ReplaceAll(s, "<", "&lt;")
}
