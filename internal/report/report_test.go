package report

import (
	"strings"
	"testing"

	"github.com/szhekpisov/preview-argocd-diff/internal/changeset"
	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

func key(name string) changeset.DocKey {
	return changeset.DocKey{Kind: discover.KindApplication, Namespace: "argocd", Name: name}
}

func TestBuildBasicSections(t *testing.T) {
	body := Build(Input{
		Changes: []ChangeReport{
			{Key: key("added-one"), Status: changeset.StatusAdded, Reasons: []string{"app added on head"}, Diff: "+ added\n"},
			{Key: key("removed-one"), Status: changeset.StatusRemoved, Reasons: []string{"app removed on base"}, Diff: "- gone\n"},
			{Key: key("modified-one"), Status: changeset.StatusModified, Reasons: []string{"targetRevision bump"}, Diff: "@@ -1 +1 @@\n-old\n+new\n"},
		},
	}, 0)

	if !strings.Contains(body, Marker) {
		t.Error("marker missing")
	}
	if !strings.Contains(body, "1 changed · 1 added · 1 removed · 0 render errors") {
		t.Errorf("summary incorrect:\n%s", body)
	}
	for _, want := range []string{"### Added Applications", "### Removed Applications", "### Changed Applications", "added-one", "removed-one", "modified-one"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body", want)
		}
	}
}

func TestBuildRenderError(t *testing.T) {
	body := Build(Input{
		Changes: []ChangeReport{
			{Key: key("broken"), Status: changeset.StatusModified, RenderErr: "chart not found"},
		},
	}, 0)
	if !strings.Contains(body, "### Render errors") {
		t.Errorf("render-errors section missing:\n%s", body)
	}
	if !strings.Contains(body, "chart not found") {
		t.Errorf("error text missing:\n%s", body)
	}
}

func TestBuildTruncation(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	changes := []ChangeReport{
		{Key: key("first"), Status: changeset.StatusModified, Diff: big},
		{Key: key("second"), Status: changeset.StatusModified, Diff: big},
		{Key: key("third"), Status: changeset.StatusModified, Diff: big},
	}
	body := Build(Input{Changes: changes, ArtifactURL: "https://example/art"}, 12_000)

	if !strings.Contains(body, "first") {
		t.Errorf("first app should be kept:\n%s", body)
	}
	if !strings.Contains(body, "_Full report truncated") {
		t.Errorf("truncation notice missing:\n%s", body)
	}
	if !strings.Contains(body, "https://example/art") {
		t.Errorf("artifact URL missing:\n%s", body)
	}
}

func TestBuildEscapesAngleBrackets(t *testing.T) {
	body := Build(Input{
		Changes: []ChangeReport{
			{Key: changeset.DocKey{Kind: discover.KindApplication, Name: "<script>"}, Status: changeset.StatusModified, Diff: "-\n+\n"},
		},
	}, 0)
	if strings.Contains(body, "<script>") {
		t.Errorf("expected angle brackets escaped:\n%s", body)
	}
}
