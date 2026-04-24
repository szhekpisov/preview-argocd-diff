package git

import (
	"io"

	"github.com/go-git/go-git/v5/plumbing/object"
)

// gitFile is a local alias so callers don't need to import go-git types.
type gitFile = object.File

func copyReader(w io.Writer, r io.Reader) (int64, error) {
	return io.Copy(w, r)
}
