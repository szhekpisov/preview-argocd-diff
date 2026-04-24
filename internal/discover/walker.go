package discover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

// Options controls discovery behavior.
type Options struct {
	// ExcludeDirs is a list of directory-name patterns matched against each
	// directory encountered during the walk (filepath.Match semantics).
	ExcludeDirs []string
	// IncludeRegex, if non-empty, is a regex that each file path (relative
	// to root) must match to be considered.
	IncludeRegex string
}

// Walk scans root recursively for YAML files, decodes every Application and
// ApplicationSet document it finds, and returns them in discovery order.
// Non-CRD documents are ignored silently. Decode errors for individual
// documents are returned as a joined error but never short-circuit the walk.
func Walk(root string, opts Options) ([]Doc, error) {
	var rx *regexp.Regexp
	if opts.IncludeRegex != "" {
		r, err := regexp.Compile(opts.IncludeRegex)
		if err != nil {
			return nil, fmt.Errorf("compile include-regex: %w", err)
		}
		rx = r
	}

	var out []Doc
	var decodeErrs []error

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if shouldSkipDir(rel, d.Name(), opts.ExcludeDirs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isYAML(d.Name()) {
			return nil
		}
		if rx != nil && !rx.MatchString(rel) {
			return nil
		}

		docs, err := decodeFile(path, rel)
		if err != nil {
			decodeErrs = append(decodeErrs, err)
			return nil
		}
		out = append(out, docs...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, errors.Join(decodeErrs...)
}

func shouldSkipDir(rel, name string, patterns []string) bool {
	// Always skip version control, dotfiles, and Helm-chart template
	// directories. Files under templates/ contain Go template syntax
	// that doesn't round-trip through a YAML parser.
	switch name {
	case ".git", ".github", "node_modules", "templates":
		return true
	}
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

type metaHeader struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items,omitempty"`
}

func decodeFile(abs, rel string) ([]Doc, error) {
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}

	// sigs.k8s.io/yaml doesn't expose a streaming multi-doc decoder, so we
	// split on document separators ourselves. This matches how kubectl reads
	// manifests.
	//
	// Per-document parse failures are silently tolerated: a single bad doc
	// in a file (or a file that happens to have a .yaml extension but isn't
	// actually YAML, e.g. a Helm template snippet caught by a weak path
	// filter) should never block discovery of valid Applications elsewhere.
	var docs []Doc
	for _, chunk := range splitYAMLDocs(raw) {
		trimmed := bytes.TrimSpace(chunk)
		if len(trimmed) == 0 {
			continue
		}

		var head metaHeader
		if err := yaml.Unmarshal(trimmed, &head); err != nil {
			continue
		}

		// List: recurse into its items.
		if head.Kind == "List" {
			for _, item := range head.Items {
				sub, err := decodeDoc(item, rel)
				if err != nil {
					continue
				}
				if sub != nil {
					docs = append(docs, *sub)
				}
			}
			continue
		}

		doc, err := decodeDoc(trimmed, rel)
		if err != nil {
			continue
		}
		if doc != nil {
			docs = append(docs, *doc)
		}
	}
	return docs, nil
}

func decodeDoc(raw []byte, file string) (*Doc, error) {
	// sigs.k8s.io/yaml converts YAML to JSON first, so raw may already be JSON.
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("yaml->json %s: %w", file, err)
	}

	var head metaHeader
	if err := json.Unmarshal(jsonBytes, &head); err != nil {
		return nil, fmt.Errorf("decode header %s: %w", file, err)
	}

	var kind Kind
	switch head.Kind {
	case "Application":
		kind = KindApplication
	case "ApplicationSet":
		kind = KindApplicationSet
	default:
		return nil, nil
	}

	var full struct {
		Spec AppSpec `json:"spec"`
	}
	if err := json.Unmarshal(jsonBytes, &full); err != nil {
		return nil, fmt.Errorf("decode spec %s (%s): %w", file, head.Metadata.Name, err)
	}

	if kind == KindApplicationSet {
		full.Spec.Generators = extractGeneratorKinds(jsonBytes)
	}

	return &Doc{
		Kind:        kind,
		Name:        head.Metadata.Name,
		Namespace:   head.Metadata.Namespace,
		Labels:      head.Metadata.Labels,
		Annotations: head.Metadata.Annotations,
		Spec:        full.Spec,
		File:        file,
	}, nil
}

// extractGeneratorKinds walks spec.generators and collects the top-level key
// names each element carries (e.g. "list", "git", "clusters"). Matrix/Merge
// generators recurse.
func extractGeneratorKinds(jsonBytes []byte) GeneratorKinds {
	var wrap struct {
		Spec struct {
			Generators []map[string]json.RawMessage `json:"generators"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(jsonBytes, &wrap); err != nil {
		return nil
	}
	out := GeneratorKinds{}
	var visit func(gens []map[string]json.RawMessage)
	visit = func(gens []map[string]json.RawMessage) {
		for _, g := range gens {
			for key, val := range g {
				out[key] = true
				if key == "matrix" || key == "merge" {
					var nested struct {
						Generators []map[string]json.RawMessage `json:"generators"`
					}
					if err := json.Unmarshal(val, &nested); err == nil {
						visit(nested.Generators)
					}
				}
			}
		}
	}
	visit(wrap.Spec.Generators)
	return out
}

// splitYAMLDocs splits a YAML byte stream on lines that are exactly `---`
// (optionally leading/trailing whitespace). Anything inside a block doesn't
// match because YAML separators must appear alone on a line.
func splitYAMLDocs(in []byte) [][]byte {
	lines := bytes.Split(in, []byte("\n"))
	var docs [][]byte
	var cur []byte
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte("---")) {
			docs = append(docs, cur)
			cur = nil
			continue
		}
		cur = append(cur, line...)
		cur = append(cur, '\n')
	}
	if len(cur) > 0 {
		docs = append(docs, cur)
	}
	return docs
}
