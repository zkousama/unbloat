package caches

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zkousama/unbloat/internal/run"
)

// notFollowed is every kind of entry that leads somewhere else. Junctions show
// up as ModeIrregular on Windows.
const notFollowed = fs.ModeSymlink | fs.ModeIrregular

// walkFiles calls fn for every regular file under dir, never following a link
// or a junction. A missing dir is not an error.
func walkFiles(dir string, fn func(path string, info fs.FileInfo)) error {
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&notFollowed != 0 || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			fn(path, info)
		}
		return nil
	})
}

// DirSize is the total size of the regular files under dir.
func DirSize(dir string) (int64, error) {
	var total int64
	err := walkFiles(dir, func(_ string, info fs.FileInfo) { total += info.Size() })
	return total, err
}

// OldFiles is the total size of the regular files under dir last modified
// before cutoff.
func OldFiles(dir string, cutoff time.Time) (int64, error) {
	var total int64
	err := walkFiles(dir, func(_ string, info fs.FileInfo) {
		if info.ModTime().Before(cutoff) {
			total += info.Size()
		}
	})
	return total, err
}

// RemoveOldFiles deletes the regular files under dir last modified before
// cutoff and returns the bytes it removed. A file that cannot be deleted,
// usually because something has it open, is skipped. Directories older than
// cutoff are then removed if they are empty; their ages are read before any
// file is deleted, because deleting a file updates its directory's time. The
// root is never removed.
func RemoveOldFiles(dir string, cutoff time.Time) (int64, error) {
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return 0, nil
	}
	var oldDirs []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir || !d.IsDir() || d.Type()&notFollowed != 0 {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) {
			oldDirs = append(oldDirs, path)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	var removed int64
	err = walkFiles(dir, func(path string, info fs.FileInfo) {
		if info.ModTime().Before(cutoff) && os.Remove(path) == nil {
			removed += info.Size()
		}
	})
	// Deepest first. os.Remove refuses a directory that is not empty.
	for i := len(oldDirs) - 1; i >= 0; i-- {
		_ = os.Remove(oldDirs[i])
	}
	return removed, err
}

// WindowsDirs are npm's caches on Windows, relative to %LOCALAPPDATA%.
var WindowsDirs = []Cache{
	{ID: "npm", Title: "npm cache", Dir: "npm-cache/_cacache"},
	{ID: "npx", Title: "npx cache", Dir: "npm-cache/_npx"},
}

func WindowsCachePath(localAppData string, c Cache) string {
	return filepath.Join(localAppData, filepath.FromSlash(c.Dir))
}

// IsWindowsCachePath reports whether path is exactly one of WindowsDirs. The
// executor checks this before deleting a directory on Windows.
func IsWindowsCachePath(localAppData, path string) bool {
	for _, c := range WindowsDirs {
		if filepath.Clean(path) == filepath.Clean(WindowsCachePath(localAppData, c)) {
			return true
		}
	}
	return false
}

// ToolCache is a cache only its own tool can clean safely.
type ToolCache struct {
	ID        string
	Title     string
	Tool      string
	PathArgs  []string
	CleanArgs []string
	FreesAll  bool // false when the clean command removes an unknown part
	Cost      string
}

// WindowsTools are the package managers whose caches are cleaned on Windows.
var WindowsTools = []ToolCache{
	{
		ID: "pnpm", Title: "pnpm store, unreferenced packages only", Tool: "pnpm",
		PathArgs: []string{"store", "path"}, CleanArgs: []string{"store", "prune"},
		Cost: "how much it frees is only known after it runs",
	},
	{
		ID: "yarn", Title: "yarn cache", Tool: "yarn",
		PathArgs: []string{"cache", "dir"}, CleanArgs: []string{"cache", "clean"},
		FreesAll: true, Cost: "rebuilt on next install",
	},
}

func FindTool(tool string) (ToolCache, bool) {
	for _, t := range WindowsTools {
		if t.Tool == tool {
			return t, true
		}
	}
	return ToolCache{}, false
}

func ToolPath(ctx context.Context, r run.Runner, t ToolCache) (string, error) {
	res, err := r.Run(ctx, t.Tool, t.PathArgs...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("%s: exit %d", run.Key(t.Tool, t.PathArgs...), res.Code)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func ToolClean(ctx context.Context, r run.Runner, t ToolCache) error {
	res, err := r.Run(ctx, t.Tool, t.CleanArgs...)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("%s: exit %d: %s", run.Key(t.Tool, t.CleanArgs...), res.Code, strings.TrimSpace(string(res.Stdout)+string(res.Stderr)))
	}
	return nil
}
