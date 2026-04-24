package differ

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltinEqual(t *testing.T) {
	d := NewBuiltin()
	out, err := d.Diff(context.Background(), "foo", []byte("same\n"), []byte("same\n"))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("equal inputs should diff to empty, got %q", out)
	}
}

func TestBuiltinChange(t *testing.T) {
	d := NewBuiltin()
	base := "line1\nline2\nline3\n"
	head := "line1\nline2-changed\nline3\n"
	out, err := d.Diff(context.Background(), "foo", []byte(base), []byte(head))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-line2") || !strings.Contains(out, "+line2-changed") {
		t.Errorf("unexpected diff output:\n%s", out)
	}
}

func TestBuiltinAddLines(t *testing.T) {
	d := NewBuiltin()
	out, _ := d.Diff(context.Background(), "foo", []byte("a\n"), []byte("a\nb\nc\n"))
	if !strings.Contains(out, "+b") || !strings.Contains(out, "+c") {
		t.Errorf("missing added lines:\n%s", out)
	}
}
