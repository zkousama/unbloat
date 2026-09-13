package caches

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zkousama/unbloat/internal/run"
)

var (
	now    = time.Date(2026, 9, 13, 4, 0, 0, 0, time.UTC)
	cutoff = now.AddDate(0, 0, -7)
	old    = now.AddDate(0, 0, -30)
	recent = now.AddDate(0, 0, -1)
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

func TestDirSizeOfAMissingDirectoryIsZero(t *testing.T) {
	got, err := DirSize(filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != 0 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestDirSizeCountsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), 100, old)
	write(t, filepath.Join(dir, "sub", "b"), 50, recent)
	got, err := DirSize(dir)
	if err != nil || got != 150 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestOldFilesCountsOnlyFilesBeforeTheCutoff(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.tmp"), 100, old)
	write(t, filepath.Join(dir, "new.tmp"), 50, recent)
	write(t, filepath.Join(dir, "deep", "old.tmp"), 10, old)
	got, err := OldFiles(dir, cutoff)
	if err != nil || got != 110 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestRemoveOldFilesKeepsRecentFilesAndRemovesEmptyOldDirectories(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old.tmp"), 100, old)
	write(t, filepath.Join(dir, "new.tmp"), 50, recent)
	write(t, filepath.Join(dir, "stale", "old.tmp"), 10, old)
	if err := os.Chtimes(filepath.Join(dir, "stale"), old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveOldFiles(dir, cutoff)
	if err != nil || removed != 110 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.tmp")); err != nil {
		t.Error("a recent file was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.tmp")); !os.IsNotExist(err) {
		t.Error("an old file was kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "stale")); !os.IsNotExist(err) {
		t.Error("an old directory left empty was kept")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Error("the root itself must never be removed")
	}
}

// A directory is only ever removed when it is empty. One still holding a recent
// file stays, however old the directory is.
func TestRemoveOldFilesNeverRemovesADirectoryWithFilesLeft(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "busy", "new.tmp"), 5, recent)
	write(t, filepath.Join(dir, "busy", "old.tmp"), 5, old)
	if err := os.Chtimes(filepath.Join(dir, "busy"), old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveOldFiles(dir, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "busy", "new.tmp")); err != nil {
		t.Fatal("the recent file and its directory must survive")
	}
}

// A link inside %TEMP% can point anywhere, including a user's projects. It is
// neither counted nor followed.
func TestLinksAreNeverFollowed(t *testing.T) {
	outside := t.TempDir()
	write(t, filepath.Join(outside, "precious"), 1000, old)
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	if got, _ := DirSize(dir); got != 0 {
		t.Errorf("DirSize followed a link: %d", got)
	}
	if got, _ := OldFiles(dir, cutoff); got != 0 {
		t.Errorf("OldFiles followed a link: %d", got)
	}
	if _, err := RemoveOldFiles(dir, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "precious")); err != nil {
		t.Fatal("a file reached through a link was deleted")
	}
}

func TestWindowsCachePaths(t *testing.T) {
	local := filepath.Join("C:", "Users", "dev", "AppData", "Local")
	npm := WindowsCachePath(local, WindowsDirs[0])
	if !IsWindowsCachePath(local, npm) {
		t.Fatalf("%s should be recognised", npm)
	}
	for _, p := range []string{local, filepath.Join(local, "npm-cache"), filepath.Join(local, "Docker")} {
		if IsWindowsCachePath(local, p) {
			t.Errorf("%s must not be treated as a cache", p)
		}
	}
}

// os.TempDir() falls back through TMP, TEMP, USERPROFILE and the Windows
// directory, so the folder the temp cleanup is given has to be checked. The
// answer is the same on every OS.
func TestIsTempDir(t *testing.T) {
	const (
		profile = `C:\Users\dev`
		windows = `C:\Windows`
		local   = `C:\Users\dev\AppData\Local`
	)
	for path, want := range map[string]bool{
		`C:\Users\dev\AppData\Local\Temp`:   true,
		`C:\Users\dev\AppData\Local\Temp\2`: true,
		`c:\users\DEV\appdata\local\temp\`:  true,
		`/tmp/unbloat-x`:                    true,

		`C:\`:                                   false,
		`D:\`:                                   false,
		`C:`:                                    false,
		`Temp`:                                  false,
		`relative\Temp`:                         false,
		`\\server\share\Temp`:                   false,
		`C:\Users\dev`:                          false,
		`C:\Users`:                              false,
		`C:\Users\dev\AppData\Local`:            false,
		`C:\Windows`:                            false,
		`C:\Windows\Temp`:                       false,
		`C:\Data`:                               false,
		``:                                      false,
		`C:\Users\dev\AppData\Local\Temp\..\..`: false,
		`\Windows\Temp`:                         false,
		`\Users\dev\AppData\Local\Temp`:         false,
	} {
		if got := IsTempDir(path, profile, windows, local); got != want {
			t.Errorf("IsTempDir(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestToolPathAndClean(t *testing.T) {
	yarn, ok := FindTool("yarn")
	if !ok {
		t.Fatal("yarn is not in WindowsTools")
	}
	f := run.NewFake().
		On(run.Out("C:\\Users\\dev\\AppData\\Local\\Yarn\\Cache\\v6\r\n"), "yarn", "cache", "dir").
		On(run.Out(""), "yarn", "cache", "clean")
	got, err := ToolPath(context.Background(), f, yarn)
	if err != nil || got != `C:\Users\dev\AppData\Local\Yarn\Cache\v6` {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := ToolClean(context.Background(), f, yarn); err != nil {
		t.Fatal(err)
	}
}

// A package manager can print an update notice before the path.
func TestToolPathIsTheLastLine(t *testing.T) {
	pnpm, _ := FindTool("pnpm")
	f := run.NewFake().On(run.Out("Update available! 9.0.0\r\nC:\\Users\\dev\\AppData\\Local\\pnpm\\store\\v3\r\n"), "pnpm", "store", "path")
	got, err := ToolPath(context.Background(), f, pnpm)
	if err != nil || got != `C:\Users\dev\AppData\Local\pnpm\store\v3` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestToolCleanReportsFailure(t *testing.T) {
	pnpm, _ := FindTool("pnpm")
	f := run.NewFake().On(run.Exit(1, "ERR_PNPM"), "pnpm", "store", "prune")
	if err := ToolClean(context.Background(), f, pnpm); err == nil {
		t.Fatal("want an error")
	}
}
