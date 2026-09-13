// Package run is the only place unbloat executes anything. Everything that
// needs a command takes a Runner, so tests can replay output recorded on a real
// machine instead of needing WSL and Docker.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Result is what a command produced. A non-zero exit is a Result, not an
// error: callers usually need the failing command's output to explain it.
type Result struct {
	Stdout []byte
	Stderr []byte
	Code   int
}

// Runner executes a command. It returns an error only when the command did
// not run at all, or the context ended it.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// Exec runs real commands and writes each one, with its output, to Log.
type Exec struct {
	Log io.Writer
	mu  sync.Mutex
}

func (e *Exec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		res.Code = exitErr.ExitCode()
		err = nil
	default:
		res.Code = -1
	}
	e.log(name, args, res, err)
	return res, err
}

func (e *Exec) log(name string, args []string, res Result, err error) {
	if e.Log == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(e.Log, "[%s] %s\nexit %d\n", time.Now().Format(time.RFC3339), Key(name, args...), res.Code)
	if err != nil {
		fmt.Fprintf(e.Log, "error: %v\n", err)
	}
	if len(res.Stdout) > 0 {
		fmt.Fprintf(e.Log, "stdout:\n%s\n", res.Stdout)
	}
	if len(res.Stderr) > 0 {
		fmt.Fprintf(e.Log, "stderr:\n%s\n", res.Stderr)
	}
}

// Key is how a command is written in logs and matched by Fake.
func Key(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}
