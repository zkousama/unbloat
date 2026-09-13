package caches

import (
	"context"
	"strings"
	"testing"

	"github.com/zkousama/unbloat/internal/run"
)

// fakeShell answers scripts by exact text, and fails the test on any script it
// was not given, so a test cannot pass by running something unplanned.
type fakeShell struct {
	t       *testing.T
	answers map[string]run.Result
	ran     []string
}

func (f *fakeShell) Script(_ context.Context, distro, script string) (run.Result, error) {
	f.ran = append(f.ran, distro+": "+script)
	res, ok := f.answers[script]
	if !ok {
		f.t.Fatalf("unexpected script in %s:\n%s", distro, script)
	}
	return res, nil
}

func TestShellQuote(t *testing.T) {
	if got := ShellQuote(`it's`); got != `'it'\''s'` {
		t.Fatalf("got %s", got)
	}
}

func TestMeasureReturnsOnlyCachesThatExist(t *testing.T) {
	script := MeasureScript(WSLCaches)
	shell := &fakeShell{t: t, answers: map[string]run.Result{
		script: run.Out("1992400\t.npm/_cacache\n1468004\t.npm/_npx\n"),
	}}
	got, err := InDistro{S: shell}.Measure(context.Background(), "Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if got["npm"] != 1992400*1024 || got["npx"] != 1468004*1024 {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["pip"]; ok {
		t.Fatal("pip was not printed, so it must not be reported")
	}
}

func TestMeasureScriptWorksFromTheHomeDirectory(t *testing.T) {
	script := MeasureScript(WSLCaches)
	if !strings.HasPrefix(script, `cd "$HOME" || exit 1`) {
		t.Fatalf("script = %s", script)
	}
	for _, c := range WSLCaches {
		if !strings.Contains(script, ShellQuote(c.Dir)) {
			t.Errorf("script does not measure %s", c.Dir)
		}
	}
}

func TestRemoveDeletesAKnownCache(t *testing.T) {
	script := RemoveScript(".npm/_npx")
	shell := &fakeShell{t: t, answers: map[string]run.Result{script: run.Out("")}}
	if err := (InDistro{S: shell}).Remove(context.Background(), "Ubuntu", ".npm/_npx"); err != nil {
		t.Fatal(err)
	}
	if script != `cd "$HOME" || exit 1; rm -rf -- '.npm/_npx'` {
		t.Fatalf("script = %s", script)
	}
}

// Remove is the one place a path becomes rm -rf. Anything that is not one of
// the known caches is refused before a shell is involved.
func TestRemoveRefusesAnythingElse(t *testing.T) {
	shell := &fakeShell{t: t, answers: map[string]run.Result{}}
	for _, dir := range []string{"/", ".", "", "..", ".npm", "projects"} {
		if err := (InDistro{S: shell}).Remove(context.Background(), "Ubuntu", dir); err == nil {
			t.Errorf("Remove(%q) should be refused", dir)
		}
	}
	if len(shell.ran) != 0 {
		t.Fatalf("a shell ran: %v", shell.ran)
	}
}

func TestRemoveReportsAFailingShell(t *testing.T) {
	script := RemoveScript(".cache/pip")
	shell := &fakeShell{t: t, answers: map[string]run.Result{script: {Code: 1, Stderr: []byte("permission denied")}}}
	err := InDistro{S: shell}.Remove(context.Background(), "Ubuntu", ".cache/pip")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestPnpmStore(t *testing.T) {
	for name, tc := range map[string]struct {
		res  run.Result
		want PnpmStore
	}{
		"pnpm and its store":  {run.Out("3900000\t/home/dev/.local/share/pnpm/store\n"), PnpmStore{Found: true, Exists: true, Size: 3900000 * 1024}},
		"a store but no pnpm": {run.Exit(3, "3900000\t/home/dev/.local/share/pnpm/store\n"), PnpmStore{Found: false, Exists: true, Size: 3900000 * 1024}},
		"pnpm but no store":   {run.Exit(4, ""), PnpmStore{Found: true}},
		"neither":             {run.Exit(5, ""), PnpmStore{}},
	} {
		shell := &fakeShell{t: t, answers: map[string]run.Result{PnpmMeasureScript: tc.res}}
		got, err := InDistro{S: shell}.Pnpm(context.Background(), "Ubuntu")
		if err != nil || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v", name, got, err, tc.want)
		}
	}
}

// A Windows pnpm reached through WSL's interop would prune the wrong store.
func TestPnpmScriptsIgnoreWindowsPnpmOnThePath(t *testing.T) {
	for _, s := range []string{PnpmMeasureScript, PnpmPruneScript} {
		if !strings.Contains(s, `""|/mnt/*)`) {
			t.Errorf("script does not reject /mnt paths:\n%s", s)
		}
	}
}

func TestPnpmPruneFailsWhenPnpmIsMissing(t *testing.T) {
	shell := &fakeShell{t: t, answers: map[string]run.Result{PnpmPruneScript: run.Exit(3, "")}}
	if err := (InDistro{S: shell}).PnpmPrune(context.Background(), "Ubuntu"); err == nil {
		t.Fatal("want an error when pnpm cannot be found")
	}
}
