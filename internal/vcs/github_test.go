package vcs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v66/github"
)

// newFakeGitHub builds a *GitHub pointed at a locally spawned HTTP server
// that mimics the three endpoints used by PostOrUpdateComment:
//
//	GET  /repos/:owner/:repo/issues/:number/comments
//	POST /repos/:owner/:repo/issues/:number/comments
//	PATCH /repos/:owner/:repo/issues/comments/:id
//
// The returned server captures the last PATCH / POST body so tests can
// assert the behavior.
type recorder struct {
	lastAction string
	lastBody   string
	existingID int64
	comments   []github.IssueComment
}

func newFakeGitHub(t *testing.T, rec *recorder) *GitHub {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/repos/acme/widgets/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = writeJSON(w, rec.comments)
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			rec.lastAction = "create"
			rec.lastBody = string(body)
			_ = writeJSON(w, github.IssueComment{ID: github.Int64(999)})
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/repos/acme/widgets/issues/comments/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.lastAction = "edit"
		rec.lastBody = string(body)
		_ = writeJSON(w, github.IssueComment{ID: github.Int64(rec.existingID)})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL + "/")
	cli := github.NewClient(nil)
	cli.BaseURL = u
	cli.UploadURL = u
	return NewGitHubWithClient(cli)
}

func writeJSON(w http.ResponseWriter, v any) error {
	_, err := fmt.Fprint(w, toJSON(v))
	return err
}

func toJSON(v any) string {
	switch x := v.(type) {
	case []github.IssueComment:
		sb := strings.Builder{}
		sb.WriteString("[")
		for i, c := range x {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, `{"id":%d,"body":%q}`, c.GetID(), c.GetBody())
		}
		sb.WriteString("]")
		return sb.String()
	case github.IssueComment:
		return fmt.Sprintf(`{"id":%d}`, x.GetID())
	}
	return "null"
}

func TestGitHubCreateComment(t *testing.T) {
	rec := &recorder{}
	gh := newFakeGitHub(t, rec)

	const marker = "<!-- preview-argocd-diff:marker -->"
	err := gh.PostOrUpdateComment(context.Background(), "acme/widgets", 42, marker, marker+"\nhello")
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if rec.lastAction != "create" {
		t.Errorf("expected create, got %q", rec.lastAction)
	}
	if !strings.Contains(rec.lastBody, "hello") {
		t.Errorf("body missing content: %q", rec.lastBody)
	}
}

func TestGitHubEditExistingComment(t *testing.T) {
	const marker = "<!-- preview-argocd-diff:marker -->"
	rec := &recorder{
		existingID: 17,
		comments: []github.IssueComment{
			{ID: github.Int64(1), Body: github.String("unrelated chatter")},
			{ID: github.Int64(17), Body: github.String(marker + "\nprevious run output")},
		},
	}
	gh := newFakeGitHub(t, rec)

	err := gh.PostOrUpdateComment(context.Background(), "acme/widgets", 42, marker, marker+"\nfresh output")
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if rec.lastAction != "edit" {
		t.Errorf("expected edit, got %q", rec.lastAction)
	}
	if !strings.Contains(rec.lastBody, "fresh output") {
		t.Errorf("body missing content: %q", rec.lastBody)
	}
}

func TestGitHubRejectsMissingMarker(t *testing.T) {
	gh := NewGitHub("")
	err := gh.PostOrUpdateComment(context.Background(), "acme/widgets", 42, "MARKER", "body without marker")
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Errorf("expected marker validation error, got %v", err)
	}
}

func TestGitHubRejectsInvalidRepo(t *testing.T) {
	gh := NewGitHub("")
	err := gh.PostOrUpdateComment(context.Background(), "not-slash", 42, "M", "M")
	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Errorf("expected owner/name error, got %v", err)
	}
}
