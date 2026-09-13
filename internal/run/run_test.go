package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a test. The other tests re-run the test binary with
// this environment variable set, so they have a real child process that exits
// with a known code on every OS.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("UNBLOAT_HELPER") {
	case "1":
		fmt.Fprint(os.Stdout, "out")
		fmt.Fprint(os.Stderr, "err")
		os.Exit(3)
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Fprint(os.Stdout, wd)
		os.Exit(0)
	case "hold":
		self, err := os.Executable()
		if err != nil {
			os.Exit(1)
		}
		grandchild := exec.Command(self, "-test.run=TestHelperProcess")
		grandchild.Env = append(os.Environ(), "UNBLOAT_HELPER=sleep")
		grandchild.Stdout = os.Stdout
		if err := grandchild.Start(); err != nil {
			os.Exit(1)
		}
		// The test needs the grandchild's pid to clean it up: this process
		// exits without waiting for it, so nothing else reaps it.
		fmt.Printf("pid %d\n", grandchild.Process.Pid)
		// Exit now, without waiting for the grandchild: it keeps the output
		// pipe this process inherited from its own parent open.
		os.Exit(0)
	case "sleep":
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
}

// A command started from a working directory that has since vanished can
// fail, so the runner starts every command from a directory it's given.
func TestExecRunsInDir(t *testing.T) {
	t.Setenv("UNBLOAT_HELPER", "pwd")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := (&Exec{Dir: dir}).Run(context.Background(), self, "-test.run=TestHelperProcess")
	if err != nil || res.Code != 0 {
		t.Fatalf("code %d, %v", res.Code, err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(string(res.Stdout))
	if got != want {
		t.Fatalf("ran in %q, want %q", res.Stdout, dir)
	}
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

// A command that has already exited 0 isn't failed just because a grandchild,
// such as a distro wsl.exe starts, still holds the output pipe open.
func TestExecTreatsAHeldPipeAfterExit0AsSuccess(t *testing.T) {
	t.Setenv("UNBLOAT_HELPER", "hold")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := (&Exec{WaitDelay: 200 * time.Millisecond}).Run(context.Background(), self, "-test.run=TestHelperProcess")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if res.Code != 0 {
		t.Fatalf("code = %d, want 0", res.Code)
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("Run took %s: it waited for the grandchild's sleep instead of the wait delay", elapsed)
	}

	// The helper's grandchild is still sleeping and holding the pipe; it
	// outlives this process unless the test kills it itself.
	var pid int
	if _, err := fmt.Sscanf(string(res.Stdout), "pid %d", &pid); err != nil {
		t.Fatalf("could not read the grandchild's pid from %q: %v", res.Stdout, err)
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill() // already exited is fine
		}
	})
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
