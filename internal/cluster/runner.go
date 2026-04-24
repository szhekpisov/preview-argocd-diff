// Package cluster manages the Kind cluster and the ArgoCD installation used
// for rendering Applications.
//
// Every external process invocation goes through the Runner interface, so
// tests can supply a fake that records calls without touching Docker or the
// network. The real implementation is execRunner, which uses os/exec.
package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Runner executes an external command. The real implementation shells out;
// tests inject a fake.
type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// Command describes one external invocation. Stdout and stderr are captured;
// Stdin may be nil.
type Command struct {
	Name string
	Args []string
	Env  []string // appended to the current environment
	Dir  string
	Stdin io.Reader
}

// Result is a captured exit status plus stdout/stderr.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// execRunner is the default Runner — it forks processes.
type execRunner struct{}

// NewExecRunner returns a Runner that actually shells out.
func NewExecRunner() Runner { return execRunner{} }

func (execRunner) Run(ctx context.Context, c Command) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	if len(c.Env) > 0 {
		cmd.Env = append(cmd.Environ(), c.Env...)
	}
	if c.Stdin != nil {
		cmd.Stdin = c.Stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{
		ExitCode: cmd.ProcessState.ExitCode(),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	if err != nil && !isExitErr(err) {
		return res, fmt.Errorf("exec %s %s: %w", c.Name, strings.Join(c.Args, " "), err)
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("%s %s exited %d: %s", c.Name, strings.Join(c.Args, " "), res.ExitCode, res.Stderr)
	}
	return res, nil
}

func isExitErr(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}
