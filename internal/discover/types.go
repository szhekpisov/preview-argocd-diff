// Package discover scans a directory tree for Argo CD Application and
// ApplicationSet manifests. Only the subset of the CRD fields needed by the
// changeset engine is decoded.
package discover

// Kind identifies the CRD variant of a discovered document.
type Kind string

const (
	KindApplication    Kind = "Application"
	KindApplicationSet Kind = "ApplicationSet"
)

// Source mirrors argoproj.io/v1alpha1 Application.spec.source (and each
// element of spec.sources). Only fields used by the changeset engine are
// represented.
type Source struct {
	RepoURL        string      `json:"repoURL,omitempty"`
	Path           string      `json:"path,omitempty"`
	TargetRevision string      `json:"targetRevision,omitempty"`
	Chart          string      `json:"chart,omitempty"`
	Ref            string      `json:"ref,omitempty"`
	Helm           *HelmSource `json:"helm,omitempty"`
}

// HelmSource mirrors Application.spec.source.helm — just the parts that can
// cause a diff. Parameters, Values, and ValuesObject are captured as opaque
// JSON so the changeset engine can tell whether they changed between refs.
type HelmSource struct {
	ValueFiles   []string `json:"valueFiles,omitempty"`
	Parameters   []byte   `json:"parameters,omitempty"`
	Values       string   `json:"values,omitempty"`
	ValuesObject []byte   `json:"valuesObject,omitempty"`
}

// GeneratorKinds is a bitmask-ish string set naming the ApplicationSet
// generators referenced in an AppSet's spec.generators list. The changeset
// engine uses this to decide whether a cluster is needed.
type GeneratorKinds map[string]bool

// AppSpec carries the single-source and multi-source forms plus optional
// ApplicationSet fields.
type AppSpec struct {
	Source     *Source        `json:"source,omitempty"`
	Sources    []Source       `json:"sources,omitempty"`
	Template   *Template      `json:"template,omitempty"`
	Generators GeneratorKinds `json:"-"`
}

// Template is the subset of ApplicationSet.spec.template we need.
type Template struct {
	Spec AppSpec `json:"spec"`
}

// Doc is one decoded Application / ApplicationSet, plus the file it came from.
type Doc struct {
	Kind        Kind
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Spec        AppSpec
	// File is the path to the YAML file the document was decoded from,
	// relative to the scan root.
	File string
}
