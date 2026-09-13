package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// openLog creates this run's log file. Every command and its output goes in
// it, and the summary prints where it is.
func openLog(localAppData string, now time.Time) (string, *os.File, error) {
	if localAppData == "" {
		return "", nil, errors.New("LOCALAPPDATA is not set")
	}
	dir := filepath.Join(localAppData, "unbloat", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, now.Format("2006-01-02T15-04-05")+".log")
	f, err := os.Create(path)
	if err != nil {
		return "", nil, err
	}
	return path, f, nil
}

// systemDrive is the drive Windows is installed on, which is where WSL and
// Docker keep their disks unless they were moved.
func systemDrive(getenv func(string) string) string {
	if d := getenv("SystemDrive"); d != "" {
		return d + `\`
	}
	return `C:\`
}

// resolveVersion is the version -ldflags set, or else the module version go
// install records. read is debug.ReadBuildInfo.
func resolveVersion(version string, read func() (*debug.BuildInfo, bool)) string {
	if version != "dev" {
		return version
	}
	if info, ok := read(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

// stat reports a regular file's size. A virtual disk in use can still be
// read this way.
func stat(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), true
}
