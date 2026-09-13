package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenLogCreatesATimestampedFileUnderLocalAppData(t *testing.T) {
	local := t.TempDir()
	path, f, err := openLog(local, time.Date(2026, 9, 13, 4, 5, 6, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := filepath.Join(local, "unbloat", "logs", "2026-09-13T04-05-06.log")
	if path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if strings.Contains(filepath.Base(path), ":") {
		t.Fatal("a colon is not allowed in a Windows file name")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenLogNeedsLocalAppData(t *testing.T) {
	if _, _, err := openLog("", time.Now()); err == nil {
		t.Fatal("want an error without LOCALAPPDATA")
	}
}

func TestSystemDrive(t *testing.T) {
	if got := systemDrive(func(string) string { return "D:" }); got != `D:\` {
		t.Errorf("got %q", got)
	}
	if got := systemDrive(func(string) string { return "" }); got != `C:\` {
		t.Errorf("got %q", got)
	}
}

func TestStatOnlyReportsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ext4.vhdx")
	if err := os.WriteFile(file, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if size, ok := stat(file); !ok || size != 64 {
		t.Errorf("file: %d %v", size, ok)
	}
	if _, ok := stat(dir); ok {
		t.Error("a directory was reported as a disk")
	}
	if _, ok := stat(filepath.Join(dir, "missing")); ok {
		t.Error("a missing file was reported")
	}
}
