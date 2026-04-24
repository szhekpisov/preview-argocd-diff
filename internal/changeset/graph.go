// Package changeset determines which Application / ApplicationSet documents
// are affected by a PR, so the caller can render only those instead of every
// app in the repo.
//
// Inputs:
//   - the discovered CRD docs from the base tree
//   - the discovered CRD docs from the head tree
//   - the list of files changed between the two trees
//
// Output: a Change per affected app, annotated with the reason(s) the app
// needs to be re-rendered.
package changeset

import (
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/szhekpisov/preview-argocd-diff/internal/discover"
)

// Status classifies an app's presence across the two trees.
type Status string

// The three possible Status values.
const (
	StatusAdded    Status = "added"
	StatusRemoved  Status = "removed"
	StatusModified Status = "modified"
)

// DocKey uniquely identifies an app across both trees.
type DocKey struct {
	Kind      discover.Kind
	Namespace string
	Name      string
}

// String returns "kind/ns/name".
func (k DocKey) String() string {
	return string(k.Kind) + "/" + k.Namespace + "/" + k.Name
}

// Change is one affected app.
type Change struct {
	Key     DocKey
	Status  Status
	Base    *discover.Doc
	Head    *discover.Doc
	Reasons []string
}

// Affected is the list of changes returned by (*Graph).Affected. Sorted by key
// for deterministic output.
type Affected []Change

// Build constructs the dependency graph and returns the affected set.
func Build(baseDocs, headDocs []discover.Doc, changedFiles []string) Affected {
	changed := make(map[string]struct{}, len(changedFiles))
	for _, f := range changedFiles {
		changed[path.Clean(f)] = struct{}{}
	}

	baseByKey := indexByKey(baseDocs)
	headByKey := indexByKey(headDocs)

	keys := make(map[DocKey]struct{})
	for k := range baseByKey {
		keys[k] = struct{}{}
	}
	for k := range headByKey {
		keys[k] = struct{}{}
	}

	var out Affected
	for k := range keys {
		b, bOK := baseByKey[k]
		h, hOK := headByKey[k]

		switch {
		case !bOK && hOK:
			out = append(out, Change{Key: k, Status: StatusAdded, Head: h, Reasons: []string{"app added on head"}})
		case bOK && !hOK:
			out = append(out, Change{Key: k, Status: StatusRemoved, Base: b, Reasons: []string{"app removed on base"}})
		case bOK && hOK:
			reasons := classify(b, h, changed)
			if len(reasons) == 0 {
				continue
			}
			out = append(out, Change{Key: k, Status: StatusModified, Base: b, Head: h, Reasons: reasons})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key.String() < out[j].Key.String() })
	return out
}

func indexByKey(docs []discover.Doc) map[DocKey]*discover.Doc {
	out := make(map[DocKey]*discover.Doc, len(docs))
	for i := range docs {
		d := &docs[i]
		out[DocKey{Kind: d.Kind, Namespace: d.Namespace, Name: d.Name}] = d
	}
	return out
}

// classify returns a non-empty slice of human-readable reasons if the app is
// affected by any changed file or by a spec delta between base and head.
func classify(base, head *discover.Doc, changed map[string]struct{}) []string {
	var reasons []string

	if base.File == head.File {
		if _, yes := changed[path.Clean(base.File)]; yes {
			reasons = append(reasons, fmt.Sprintf("CRD file %s changed", base.File))
		}
	} else {
		if _, yes := changed[path.Clean(base.File)]; yes {
			reasons = append(reasons, fmt.Sprintf("CRD file %s changed on base", base.File))
		}
		if _, yes := changed[path.Clean(head.File)]; yes {
			reasons = append(reasons, fmt.Sprintf("CRD file %s changed on head", head.File))
		}
	}

	reasons = append(reasons, checkSources(head.Spec, changed)...)
	reasons = append(reasons, specDeltas(&base.Spec, &head.Spec)...)

	if head.Kind == discover.KindApplicationSet && head.Spec.Template != nil {
		reasons = append(reasons, checkSources(head.Spec.Template.Spec, changed)...)
	}

	return dedup(reasons)
}

// checkSources scans both the single-source and multi-source forms and
// reports every changed file that falls under the app's source path or is
// referenced directly via helm.valueFiles.
func checkSources(spec discover.AppSpec, changed map[string]struct{}) []string {
	var reasons []string
	srcs := spec.Sources
	if spec.Source != nil {
		srcs = append([]discover.Source{*spec.Source}, srcs...)
	}
	for _, s := range srcs {
		base := strings.TrimSuffix(path.Clean(s.Path), "/")
		if base != "" && base != "." {
			for f := range changed {
				if underDir(f, base) {
					reasons = append(reasons, fmt.Sprintf("file %s under source path %s changed", f, base))
				}
			}
		}
		if s.Helm != nil {
			for _, vf := range s.Helm.ValueFiles {
				candidates := []string{path.Clean(vf)}
				if base != "" && base != "." && !path.IsAbs(vf) && !strings.HasPrefix(vf, "$") {
					candidates = append(candidates, path.Join(base, vf))
				}
				for _, c := range candidates {
					if _, yes := changed[c]; yes {
						reasons = append(reasons, fmt.Sprintf("value file %s changed", c))
					}
				}
			}
		}
	}
	return reasons
}

// specDeltas compares base and head specs and reports the fields that moved.
// It's intentionally cheap: we care about whether there's *any* delta, not the
// exact diff (that comes out of the renderer later).
func specDeltas(b, h *discover.AppSpec) []string {
	var reasons []string

	bs := normalizeSources(b)
	hs := normalizeSources(h)

	for i := 0; i < max(len(bs), len(hs)); i++ {
		var bv, hv *discover.Source
		if i < len(bs) {
			bv = &bs[i]
		}
		if i < len(hs) {
			hv = &hs[i]
		}
		switch {
		case bv == nil && hv != nil:
			reasons = append(reasons, fmt.Sprintf("source[%d] added", i))
		case bv != nil && hv == nil:
			reasons = append(reasons, fmt.Sprintf("source[%d] removed", i))
		default:
			if bv.TargetRevision != hv.TargetRevision {
				reasons = append(reasons,
					fmt.Sprintf("targetRevision %q -> %q", bv.TargetRevision, hv.TargetRevision))
			}
			if bv.Chart != hv.Chart {
				reasons = append(reasons, fmt.Sprintf("chart %q -> %q", bv.Chart, hv.Chart))
			}
			if bv.RepoURL != hv.RepoURL {
				reasons = append(reasons, fmt.Sprintf("repoURL %q -> %q", bv.RepoURL, hv.RepoURL))
			}
			if bv.Path != hv.Path {
				reasons = append(reasons, fmt.Sprintf("source path %q -> %q", bv.Path, hv.Path))
			}
			if !reflect.DeepEqual(bv.Helm, hv.Helm) {
				reasons = append(reasons, "helm block changed")
			}
		}
	}

	return reasons
}

func normalizeSources(s *discover.AppSpec) []discover.Source {
	if s == nil {
		return nil
	}
	if len(s.Sources) > 0 {
		return s.Sources
	}
	if s.Source != nil {
		return []discover.Source{*s.Source}
	}
	return nil
}

// underDir reports whether file is inside dir (or equals it).
func underDir(file, dir string) bool {
	file = path.Clean(file)
	dir = strings.TrimSuffix(path.Clean(dir), "/")
	if file == dir {
		return true
	}
	return strings.HasPrefix(file, dir+"/")
}

func dedup(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
