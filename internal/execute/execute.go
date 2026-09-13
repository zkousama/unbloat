// Package execute carries out the steps of a plan on the real machine.
package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zkousama/unbloat/internal/caches"
	"github.com/zkousama/unbloat/internal/docker"
	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/run"
	"github.com/zkousama/unbloat/internal/scan"
	"github.com/zkousama/unbloat/internal/sys"
	"github.com/zkousama/unbloat/internal/vhd"
	"github.com/zkousama/unbloat/internal/wsl"
)

// everything is a cutoff no file is newer than, so RemoveOldFiles empties a
// whole cache while still never following a link.
var everything = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// Executor runs steps. It satisfies plan.Executor.
type Executor struct {
	WSL          wsl.Client
	Docker       *docker.Client // nil when docker.exe was not found
	Runner       run.Runner
	Compactor    vhd.Compactor
	Facts        sys.Facts
	LocalAppData string
	Stat         func(path string) (int64, bool)
	Now          func() time.Time
	Sleep        func(time.Duration)
	UnlockTries  int  // how many times to check a disk is free, a second apart
	Optimize     bool // whether Optimize-VHD exists; only Describe reads it
}

func (e Executor) Execute(ctx context.Context, s plan.Step) (int64, error) {
	it := s.Item
	switch s.Phase {
	case plan.PhaseTrim:
		return 0, e.WSL.Trim(ctx, it.Distro, it.Mount)
	case plan.PhaseStop:
		return 0, e.stop(ctx)
	case plan.PhaseUnlock:
		return 0, e.unlock(ctx, it.Path)
	case plan.PhaseCompact:
		return e.compact(ctx, it.Path)
	}

	switch it.Kind {
	case plan.KindWindowsTemp:
		return caches.RemoveOldFiles(it.Path, e.Now().Add(-scan.TempAge))

	case plan.KindWindowsRemove:
		if !caches.IsWindowsCachePath(e.LocalAppData, it.Path) {
			return 0, fmt.Errorf("refusing to delete %s: it is not a cache unbloat knows", it.Path)
		}
		return caches.RemoveOldFiles(it.Path, everything)

	case plan.KindWindowsTool:
		t, ok := caches.FindTool(it.Tool)
		if !ok {
			return 0, fmt.Errorf("%s is not a package manager unbloat knows", it.Tool)
		}
		before, _ := caches.DirSize(it.Path)
		if err := caches.ToolClean(ctx, e.Runner, t); err != nil {
			return 0, err
		}
		after, _ := caches.DirSize(it.Path)
		return max(before-after, 0), nil

	case plan.KindWSLRemove:
		if err := (caches.InDistro{S: e.WSL}).Remove(ctx, it.Distro, it.Path); err != nil {
			return 0, err
		}
		return it.Size, nil

	case plan.KindWSLPnpmPrune:
		inside := caches.InDistro{S: e.WSL}
		if err := inside.PnpmPrune(ctx, it.Distro); err != nil {
			return 0, err
		}
		after, err := inside.Pnpm(ctx, it.Distro)
		if err != nil {
			return 0, nil // pruned, but the new size could not be read
		}
		return max(it.Size-after.Size, 0), nil

	case plan.KindDockerBuildCache, plan.KindDockerImages:
		if e.Docker == nil {
			return 0, errors.New("docker.exe was not found")
		}
		prune := e.Docker.PruneBuildCache
		if it.Kind == plan.KindDockerImages {
			prune = e.Docker.PruneImages
		}
		if err := prune(ctx); err != nil {
			return 0, err
		}
		return it.Frees, nil
	}
	return 0, fmt.Errorf("unbloat does not know how to run %q", s.ID)
}

// stop stops Docker Desktop with its own command, then shuts WSL down. If
// Docker Desktop does not stop, WSL is left running and nothing is compacted.
func (e Executor) stop(ctx context.Context) error {
	if e.Docker != nil && e.Docker.Running(ctx) {
		if err := e.Docker.Stop(ctx); err != nil {
			return fmt.Errorf("Docker Desktop did not stop, so nothing was shut down. Quit it from its tray icon and run unbloat again. (%v)", err)
		}
	}
	return e.WSL.Shutdown(ctx)
}

func (e Executor) unlock(ctx context.Context, path string) error {
	sleep := e.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	tries := max(e.UnlockTries, 1)
	for i := 1; ; i++ {
		locked, err := e.Facts.Locked(path)
		if err != nil {
			return fmt.Errorf("checking whether %s is in use: %w", path, err)
		}
		if !locked {
			return nil
		}
		if i >= tries || ctx.Err() != nil {
			break
		}
		sleep(time.Second)
	}
	return fmt.Errorf("%s is still in use. An open WSL window, Docker Desktop, or an editor attached to WSL usually holds it", path)
}

func (e Executor) compact(ctx context.Context, path string) (int64, error) {
	before, _ := e.Stat(path)
	if err := e.Compactor.Compact(ctx, path); err != nil {
		return 0, err
	}
	after, ok := e.Stat(path)
	if !ok {
		return 0, fmt.Errorf("compacted %s, but its size could not be read afterwards", path)
	}
	return max(before-after, 0), nil
}

func inDistro(distro string, args ...string) string {
	return run.Key("wsl.exe", append([]string{"-d", distro}, args...)...)
}

// Describe returns the commands a step runs, as the confirmation screen shows
// them. Steps that are not a command are described in words.
func (e Executor) Describe(s plan.Step) []string {
	it := s.Item
	switch s.Phase {
	case plan.PhaseTrim:
		if it.Mount == "" {
			return []string{inDistro(it.Distro, "-u", "root", "-e", "fstrim", "-av")}
		}
		return []string{inDistro(it.Distro, "-u", "root", "-e", "fstrim", "-v", it.Mount)}
	case plan.PhaseStop:
		return []string{"docker desktop stop   (if Docker Desktop is running)", "wsl.exe --shutdown"}
	case plan.PhaseUnlock:
		return []string{"check that no process has " + it.Path + " open"}
	case plan.PhaseCompact:
		return vhd.Commands(it.Path, e.Optimize)
	}

	switch it.Kind {
	case plan.KindWindowsTemp:
		return []string{"delete files older than 7 days under " + it.Path + ", skipping open files and never following links"}
	case plan.KindWindowsRemove:
		return []string{"delete the files under " + it.Path + ", never following links"}
	case plan.KindWindowsTool:
		if t, ok := caches.FindTool(it.Tool); ok {
			return []string{run.Key(t.Tool, t.CleanArgs...)}
		}
	case plan.KindWSLRemove:
		return []string{inDistro(it.Distro, "-e", "sh", "-c") + ":", "  " + caches.RemoveScript(it.Path)}
	case plan.KindWSLPnpmPrune:
		return []string{inDistro(it.Distro, "-e", "sh", "-c") + ":", "  " + caches.PnpmPruneScript}
	case plan.KindDockerBuildCache:
		return []string{"docker builder prune -af"}
	case plan.KindDockerImages:
		return []string{"docker image prune -af"}
	}
	return nil
}
