// Package differ renders a text diff between the base and head manifests for
// one app. The Differ interface abstracts over the built-in unified-diff
// implementation and (in a future milestone) external tools invoked via
// os/exec with a configurable command template.
package differ

import (
	"context"
	"fmt"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

// Differ produces a unified-ish text diff between two byte slices. An empty
// return string means "no diff".
type Differ interface {
	Diff(ctx context.Context, name string, base, head []byte) (string, error)
}

// Builtin is the default Differ. It uses hexops/gotextdiff to produce
// unified-diff output with a fixed context size.
type Builtin struct {
	// ContextLines is the number of lines of context around each hunk.
	// Zero means the gotextdiff default (3).
	ContextLines int
}

// NewBuiltin returns a Differ with sensible defaults.
func NewBuiltin() *Builtin { return &Builtin{} }

// Diff implements Differ.
func (b *Builtin) Diff(_ context.Context, name string, base, head []byte) (string, error) {
	if string(base) == string(head) {
		return "", nil
	}
	edits := myers.ComputeEdits(span.URIFromPath(name), string(base), string(head))
	if len(edits) == 0 {
		return "", nil
	}
	unified := gotextdiff.ToUnified(name+".base", name+".head", string(base), edits)
	out := fmt.Sprint(unified)
	return out, nil
}
