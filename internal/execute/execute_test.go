package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zkousama/unbloat/internal/caches"
	"github.com/zkousama/unbloat/internal/docker"
	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/run"
	"github.com/zkousama/unbloat/internal/sys"
	"github.com/zkousama/unbloat/internal/vhd"
	"github.com/zkousama/unbloat/internal/wsl"
)

const (
	dockerExe = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	disk      = `C:\Users\dev\AppData\Local\Docker\wsl\disk\docker_data.vhdx`
)

func write(t *testing.T, path string, size int, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func windowsStep(kind plan.Kind, path string) plan.Step {
	return plan.Step{ID: "win:x", Title: "x", Phase: plan.PhaseWindowsCaches, Item: plan.Item{Kind: kind, Path: path}}
}

func TestWindowsRemovalRefusesAnythingButAKnownCache(t *testing.T) {
	local := t.TempDir()
	write(t, filepath.Join(local, "Docker", "important"), 10, time.Now())
	e := Executor{LocalAppData: local, Now: time.Now}

	for _, path := range []string{local, filepath.Join(local, "Docker"), filepath.Join(local, "npm-cache")} {
		if _, err := e.Execute(context.Background(), windowsStep(plan.KindWindowsRemove, path)); err == nil {
			t.Errorf("deleting %s was not refused", path)
		}
	}
	if _, err := os.Stat(filepath.Join(local, "Docker", "important")); err != nil {
		t.Fatal("a file outside the caches was deleted")
	}
}

func TestWindowsRemovalEmptiesTheCache(t *testing.T) {
	local := t.TempDir()
	cache := caches.WindowsCachePath(local, caches.WindowsDirs[0])
	write(t, filepath.Join(cache, "index-v5", "ab", "entry"), 300, time.Now())
	e := Executor{LocalAppData: local, Now: time.Now}

	freed, err := e.Execute(context.Background(), windowsStep(plan.KindWindowsRemove, cache))
	if err != nil || freed != 300 {
		t.Fatalf("freed %d, %v", freed, err)
	}
	if size, _ := caches.DirSize(cache); size != 0 {
		t.Fatalf("%d bytes left", size)
	}
}

func TestTempRemovalKeepsRecentFiles(t *testing.T) {
	temp := t.TempDir()
	now := time.Now()
	write(t, filepath.Join(temp, "old.tmp"), 100, now.AddDate(0, 0, -30))
	write(t, filepath.Join(temp, "new.tmp"), 50, now.Add(-time.Hour))
	e := Executor{Now: func() time.Time { return now }}

	freed, err := e.Execute(context.Background(), windowsStep(plan.KindWindowsTemp, temp))
	if err != nil || freed != 100 {
		t.Fatalf("freed %d, %v", freed, err)
	}
	if _, err := os.Stat(filepath.Join(temp, "new.tmp")); err != nil {
		t.Fatal("a recent file was deleted")
	}
}

func TestWSLCacheRemovalRunsTheKnownScript(t *testing.T) {
	f := run.NewFake().On(run.Out(""), "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", caches.RemoveScript(".npm/_npx"))
	s := plan.Step{ID: "cache:Ubuntu:npx", Phase: plan.PhaseWSLCaches,
		Item: plan.Item{Kind: plan.KindWSLRemove, Distro: "Ubuntu", Path: ".npm/_npx", Size: 1500}}

	freed, err := Executor{WSL: wsl.Client{R: f}}.Execute(context.Background(), s)
	if err != nil || freed != 1500 {
		t.Fatalf("freed %d, %v", freed, err)
	}
}

func TestTrimRunsAsRootInTheDistroThatOwnsTheDisk(t *testing.T) {
	f := run.NewFake().
		On(run.Out(""), "wsl.exe", "-d", "Ubuntu", "-u", "root", "-e", "fstrim", "-av").
		On(run.Out(""), "wsl.exe", "-d", docker.DistroName, "-u", "root", "-e", "fstrim", "-v", docker.DataMount)
	e := Executor{WSL: wsl.Client{R: f}}
	p := plan.Plan{Items: []plan.Item{
		{ID: "compact:Ubuntu", Kind: plan.KindCompact, Disk: "Ubuntu", Distro: "Ubuntu", Selected: true},
		{ID: "compact:docker", Kind: plan.KindCompact, Disk: "docker", Distro: docker.DistroName, Mount: docker.DataMount, Selected: true},
	}}
	for _, s := range p.Steps() {
		if s.Phase != plan.PhaseTrim {
			continue
		}
		if _, err := e.Execute(context.Background(), s); err != nil {
			t.Errorf("%s: %v", s.ID, err)
		}
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls = %v", f.Calls())
	}
}

var stop = plan.Step{ID: "stop", Phase: plan.PhaseStop}

const (
	desktopUp   = "\"Docker Desktop.exe\",\"10432\",\"Console\",\"1\",\"152,344 K\"\r\n"
	desktopDown = "INFO: No tasks are running which match the specified criteria.\r\n"
)

func tasklist(f *run.Fake, out string) *run.Fake {
	return f.On(run.Out(out), "tasklist.exe", "/FI", "IMAGENAME eq Docker Desktop.exe", "/FO", "CSV", "/NH")
}

func TestStopStopsDockerDesktopBeforeWSL(t *testing.T) {
	f := tasklist(run.NewFake(), desktopUp).
		On(run.Out("29.6.1"), dockerExe, "info", "--format", "{{.ServerVersion}}").
		On(run.Out(""), dockerExe, "desktop", "stop").
		On(run.Out(""), "wsl.exe", "--shutdown")
	e := Executor{WSL: wsl.Client{R: f}, Docker: &docker.Client{R: f, Exe: dockerExe}, Runner: f}

	if _, err := e.Execute(context.Background(), stop); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 4 || !strings.HasSuffix(calls[2], "desktop stop") || calls[3] != "wsl.exe --shutdown" {
		t.Fatalf("calls = %v", calls)
	}
}

// Nothing is compacted without a clean stop, and WSL is not shut down under a
// Docker Desktop that is still running.
func TestAFailedDockerStopLeavesWSLRunning(t *testing.T) {
	f := tasklist(run.NewFake(), desktopUp).
		On(run.Out("29.6.1"), dockerExe, "info", "--format", "{{.ServerVersion}}").
		On(run.Exit(1, "timed out"), dockerExe, "desktop", "stop")
	e := Executor{WSL: wsl.Client{R: f}, Docker: &docker.Client{R: f, Exe: dockerExe}, Runner: f}

	_, err := e.Execute(context.Background(), stop)
	if err == nil || !strings.Contains(err.Error(), "tray") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range f.Calls() {
		if c == "wsl.exe --shutdown" {
			t.Fatal("WSL was shut down after Docker Desktop failed to stop")
		}
	}
}

func TestStopWithoutDockerOnlyShutsDownWSL(t *testing.T) {
	f := tasklist(run.NewFake(), desktopDown).On(run.Out(""), "wsl.exe", "--shutdown")
	e := Executor{WSL: wsl.Client{R: f}, Runner: f}
	if _, err := e.Execute(context.Background(), stop); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[1] != "wsl.exe --shutdown" {
		t.Fatalf("calls = %v", calls)
	}
}

// A hung, starting or failing engine is not a stopped Docker Desktop: the
// process still shows up in tasklist, so it is still stopped properly.
func TestAHungEngineStillStopsDockerDesktopFirst(t *testing.T) {
	f := tasklist(run.NewFake(), desktopUp).
		On(run.Exit(1, ""), dockerExe, "info", "--format", "{{.ServerVersion}}").
		On(run.Out(""), dockerExe, "desktop", "stop").
		On(run.Out(""), "wsl.exe", "--shutdown")
	e := Executor{WSL: wsl.Client{R: f}, Docker: &docker.Client{R: f, Exe: dockerExe}, Runner: f}

	if _, err := e.Execute(context.Background(), stop); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	stopIdx, shutdownIdx := -1, -1
	for i, c := range calls {
		if strings.HasSuffix(c, "desktop stop") {
			stopIdx = i
		}
		if c == "wsl.exe --shutdown" {
			shutdownIdx = i
		}
	}
	if stopIdx == -1 || shutdownIdx == -1 || stopIdx > shutdownIdx {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDockerDesktopWithoutDockerExeLeavesWSLRunning(t *testing.T) {
	f := tasklist(run.NewFake(), desktopUp)
	e := Executor{WSL: wsl.Client{R: f}, Runner: f}

	_, err := e.Execute(context.Background(), stop)
	if err == nil || !strings.Contains(err.Error(), "tray") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range f.Calls() {
		if c == "wsl.exe --shutdown" {
			t.Fatal("WSL was shut down while Docker Desktop was running")
		}
	}
}

func TestUnreadableTasklistCountsAsDockerDesktopRunning(t *testing.T) {
	f := run.NewFake().Fails(errors.New("not found"), "tasklist.exe", "/FI", "IMAGENAME eq Docker Desktop.exe", "/FO", "CSV", "/NH")
	e := Executor{WSL: wsl.Client{R: f}, Runner: f}

	_, err := e.Execute(context.Background(), stop)
	if err == nil {
		t.Fatal("expected an error when tasklist could not answer")
	}
	for _, c := range f.Calls() {
		if c == "wsl.exe --shutdown" {
			t.Fatal("WSL was shut down when tasklist could not answer")
		}
	}
}

func unlockStep() plan.Step {
	return plan.Step{ID: "unlock:docker", Phase: plan.PhaseUnlock, Item: plan.Item{Path: disk}}
}

// The virtual machine takes a few seconds to let go of its disks after
// wsl --shutdown returns, so the check waits before giving up.
func TestUnlockWaitsThenNamesWhatUsuallyHoldsTheDisk(t *testing.T) {
	slept := 0
	e := Executor{
		Facts:       &sys.FakeFacts{LockedPaths: map[string]bool{disk: true}},
		Sleep:       func(time.Duration) { slept++ },
		UnlockTries: 3,
	}
	_, err := e.Execute(context.Background(), unlockStep())
	if err == nil || !strings.Contains(err.Error(), "WSL window") {
		t.Fatalf("err = %v", err)
	}
	if slept != 2 {
		t.Fatalf("slept %d times, want 2", slept)
	}
}

func TestUnlockPassesWhenTheDiskIsFree(t *testing.T) {
	e := Executor{Facts: &sys.FakeFacts{}, Sleep: func(time.Duration) { t.Fatal("waited for a free disk") }, UnlockTries: 3}
	if _, err := e.Execute(context.Background(), unlockStep()); err != nil {
		t.Fatal(err)
	}
}

func TestCompactReportsHowMuchTheFileShrank(t *testing.T) {
	f := run.NewFake().
		On(run.Exit(1, ""), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", vhd.HasOptimizeScript).
		On(run.Out(""), "diskpart.exe", "/s", `C:\Temp\s.txt`)
	sizes := []int64{27_000_000_000, 19_000_000_000}
	e := Executor{
		Compactor: vhd.Compactor{R: f, WriteScript: func(string) (string, func(), error) {
			return `C:\Temp\s.txt`, func() {}, nil
		}},
		Stat: func(string) (int64, bool) { s := sizes[0]; sizes = sizes[1:]; return s, true },
	}
	s := plan.Step{ID: "compact:docker", Phase: plan.PhaseCompact, Critical: true, Item: plan.Item{Path: disk}}
	freed, err := e.Execute(context.Background(), s)
	if err != nil || freed != 8_000_000_000 {
		t.Fatalf("freed %d, %v", freed, err)
	}
}

func TestDescribeCoversEveryStep(t *testing.T) {
	p := plan.Plan{Items: []plan.Item{
		{ID: "win:temp", Kind: plan.KindWindowsTemp, Path: `C:\Temp`, Selected: true},
		{ID: "win:npm", Kind: plan.KindWindowsRemove, Path: `C:\npm`, Selected: true},
		{ID: "win:yarn", Kind: plan.KindWindowsTool, Tool: "yarn", Selected: true},
		{ID: "cache:Ubuntu:npm", Kind: plan.KindWSLRemove, Distro: "Ubuntu", Path: ".npm/_cacache", Selected: true},
		{ID: "cache:Ubuntu:pnpm", Kind: plan.KindWSLPnpmPrune, Distro: "Ubuntu", Selected: true},
		{ID: "docker:buildcache", Kind: plan.KindDockerBuildCache, Selected: true},
		{ID: "docker:images", Kind: plan.KindDockerImages, Selected: true},
		{ID: "compact:docker", Kind: plan.KindCompact, Disk: "docker", Distro: docker.DistroName, Mount: docker.DataMount, Path: disk, Selected: true},
	}}
	e := Executor{}
	for _, s := range p.Steps() {
		lines := e.Describe(s)
		if len(lines) == 0 {
			t.Errorf("%s has no description", s.ID)
		}
		if s.ID == "cache:Ubuntu:npm" && !strings.Contains(strings.Join(lines, " "), "rm -rf -- '.npm/_cacache'") {
			t.Errorf("%s: %v", s.ID, lines)
		}
	}
}
