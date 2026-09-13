package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestHelperProcess is not a test. The other tests re-run the test binary with
// this environment variable set, so they have a real child process that exits
// with a known code on every OS.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("UNBLOAT_HELPER") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, "out")
	fmt.Fprint(os.Stderr, "err")
	os.Exit(3)
}

func TestExecReportsANonZeroExitAsAResult(t *testing.T) {
	t.Setenv("UNBLOAT_HELPER", "1")
	var log bytes.Buffer
	e := &Exec{Log: &log}

	res, err := e.Run(context.Background(), os.Args[0], "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatalf("a non-zero exit should not be an error, got %v", err)
	}
	if res.Code != 3 {
		t.Fatalf("code = %d, want 3", res.Code)
	}
	if string(res.Stdout) != "out" || string(res.Stderr) != "err" {
		t.Fatalf("stdout %q stderr %q", res.Stdout, res.Stderr)
	}
	if !strings.Contains(log.String(), "exit 3") {
		t.Fatalf("log does not record the exit code:\n%s", log.String())
	}
}

func TestExecReturnsAnErrorWhenTheCommandCannotStart(t *testing.T) {
	e := &Exec{}
	res, err := e.Run(context.Background(), "unbloat-no-such-command")
	if err == nil {
		t.Fatal("want an error for a missing executable")
	}
	if res.Code != -1 {
		t.Fatalf("code = %d, want -1", res.Code)
	}
}

func TestFakeReplaysWhatWasRecorded(t *testing.T) {
	f := NewFake().On(Out("hello"), "wsl.exe", "--list", "--quiet")
	res, err := f.Run(context.Background(), "wsl.exe", "--list", "--quiet")
	if err != nil || string(res.Stdout) != "hello" {
		t.Fatalf("got %q, %v", res.Stdout, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "wsl.exe --list --quiet" {
		t.Fatalf("calls = %v", got)
	}
}

func TestFakeFailsLoudlyOnAnUnrecordedCommand(t *testing.T) {
	f := NewFake()
	if _, err := f.Run(context.Background(), "docker.exe", "info"); err == nil {
		t.Fatal("an unrecorded command must fail the caller")
	}
}

func TestFakeCanSimulateAMissingExecutable(t *testing.T) {
	missing := errors.New("not found")
	f := NewFake().Fails(missing, "pnpm", "store", "path")
	if _, err := f.Run(context.Background(), "pnpm", "store", "path"); !errors.Is(err, missing) {
		t.Fatalf("err = %v", err)
	}
}
