// Package scan builds a plan from the machine. It only reads: nothing here
// deletes, stops or starts anything, and a stopped distro stays stopped.
package scan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zkousama/unbloat/internal/caches"
	"github.com/zkousama/unbloat/internal/docker"
	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/run"
	"github.com/zkousama/unbloat/internal/sys"
	"github.com/zkousama/unbloat/internal/wsl"
)

// TempAge is how old a file in %TEMP% has to be before it is offered.
const TempAge = 7 * 24 * time.Hour

const (
	sectionWindows = "Caches on Windows"
	sectionDocker  = "Docker"
	sectionDisks   = "Disks"
	sectionFailed  = "Not scanned"

	compactCost = "closes every WSL window and stops Docker Desktop"
)

func sectionWSL(distro string) string { return "Caches inside WSL (" + distro + ")" }

// Sources is everything a scan reads.
type Sources struct {
	WSL          wsl.Client
	Docker       *docker.Client // nil when docker.exe was not found
	Runner       run.Runner     // runs package managers on Windows
	Facts        sys.Facts
	LookPath     func(file string) (string, error)
	Stat         func(path string) (size int64, ok bool)
	LocalAppData string
	UserProfile  string
	SystemRoot   string
	Temp         string
	Now          time.Time
}

// Report is called as each part of the scan finishes. err is nil on success.
type Report func(target string, err error)

type scanner struct {
	src     Sources
	report  Report
	items   []plan.Item
	disks   []plan.Item
	failed  []plan.Item
	running []string
	docker  bool
}

// Scan measures the machine and returns every item it found. A part that
// fails becomes a note under "Not scanned" and the rest of the scan carries on.
func Scan(ctx context.Context, src Sources, report Report) plan.Plan {
	if report == nil {
		report = func(string, error) {}
	}
	s := &scanner{src: src, report: report}

	s.windows(ctx)
	distros := s.distros(ctx)
	for _, d := range distros {
		if !docker.IsDockerDistro(d.Name) {
			s.distro(ctx, d)
		}
	}
	s.dockerDesktop(ctx, distros)

	items := append(append(s.items, s.disks...), s.failed...)
	return plan.Plan{Items: items, Running: s.running, DockerRunning: s.docker}
}

func (s *scanner) fail(target string, err error) {
	s.failed = append(s.failed, plan.Item{
		ID: "failed:" + target, Section: sectionFailed, Kind: plan.KindNote,
		Title: target, Detail: err.Error(),
	})
	s.report(target, err)
}

func (s *scanner) windows(ctx context.Context) {
	if s.src.Temp != "" {
		if !caches.IsTempDir(s.src.Temp, s.src.UserProfile, s.src.SystemRoot, s.src.LocalAppData) {
			s.items = append(s.items, plan.Item{
				ID: "win:temp:refused", Section: sectionWindows, Kind: plan.KindNote,
				Title:  "%TEMP% is not offered",
				Detail: "It points at " + s.src.Temp + ", which doesn't look like a temp folder.",
			})
		} else if old, err := caches.OldFiles(s.src.Temp, s.src.Now.Add(-TempAge)); err != nil {
			s.fail("%TEMP%", err)
		} else if old > 0 {
			s.items = append(s.items, plan.Item{
				ID: "win:temp", Section: sectionWindows, Kind: plan.KindWindowsTemp,
				Title: "%TEMP%, files older than 7 days", Size: old, Frees: old, Lands: plan.OnC,
				Cost: "files still open are skipped", Selected: true, Path: s.src.Temp,
			})
		}
	}

	if s.src.LocalAppData != "" {
		for _, c := range caches.WindowsDirs {
			path := caches.WindowsCachePath(s.src.LocalAppData, c)
			size, err := caches.DirSize(path)
			if err != nil {
				s.fail(c.Title+" on Windows", err)
				continue
			}
			if size > 0 {
				s.items = append(s.items, plan.Item{
					ID: "win:" + c.ID, Section: sectionWindows, Kind: plan.KindWindowsRemove,
					Title: c.Title, Size: size, Frees: size, Lands: plan.OnC,
					Cost: "rebuilt on the next install", Selected: true, Path: path,
				})
			}
		}
	}

	for _, t := range caches.WindowsTools {
		if _, err := s.src.LookPath(t.Tool); err != nil {
			continue
		}
		path, err := caches.ToolPath(ctx, s.src.Runner, t)
		if err != nil {
			s.fail(t.Title+" on Windows", err)
			continue
		}
		size, err := caches.DirSize(path)
		if err != nil {
			s.fail(t.Title+" on Windows", err)
			continue
		}
		if size == 0 {
			continue
		}
		frees := int64(0)
		if t.FreesAll {
			frees = size
		}
		s.items = append(s.items, plan.Item{
			ID: "win:" + t.ID, Section: sectionWindows, Kind: plan.KindWindowsTool,
			Title: t.Title, Size: size, Frees: frees, Lands: plan.OnC,
			Cost: t.Cost, Selected: true, Path: path, Tool: t.Tool,
		})
	}
	s.report(sectionWindows, nil)
}

func (s *scanner) distros(ctx context.Context) []wsl.Distro {
	distros, err := s.src.WSL.Distros(ctx)
	if err != nil {
		s.fail("WSL", err)
		return nil
	}
	for _, d := range distros {
		if d.Running && !docker.IsDockerDistro(d.Name) {
			s.running = append(s.running, d.Name)
		}
	}
	s.report("WSL", nil)
	return distros
}

func (s *scanner) distro(ctx context.Context, d wsl.Distro) {
	section := sectionWSL(d.Name)
	used := int64(-1)

	if !d.Running {
		s.items = append(s.items, plan.Item{
			ID: "wsl:" + d.Name + ":stopped", Section: section, Kind: plan.KindNote,
			Title:  d.Name + " is stopped",
			Detail: "Its caches are not measured, because measuring would start it.",
		})
	} else {
		inside := caches.InDistro{S: s.src.WSL}
		if sizes, err := inside.Measure(ctx, d.Name); err != nil {
			s.fail(d.Name+" caches", err)
		} else {
			for _, c := range caches.WSLCaches {
				if size := sizes[c.ID]; size > 0 {
					s.items = append(s.items, plan.Item{
						ID: "cache:" + d.Name + ":" + c.ID, Section: section, Kind: plan.KindWSLRemove,
						Title: c.Title, Size: size, Frees: size, Lands: plan.InsideDisk, Disk: d.Name,
						Cost: "rebuilt on the next install", Selected: true, Distro: d.Name, Path: c.Dir,
					})
				}
			}
		}

		if store, err := inside.Pnpm(ctx, d.Name); err != nil {
			s.fail(d.Name+" pnpm store", err)
		} else if store.Exists && store.Size > 0 {
			it := plan.Item{
				ID: "cache:" + d.Name + ":pnpm", Section: section, Kind: plan.KindWSLPnpmPrune,
				Title: "pnpm store, unreferenced packages only", Size: store.Size, Lands: plan.InsideDisk, Disk: d.Name,
				Cost: "how much it frees is only known after it runs", Selected: store.Found, Distro: d.Name,
			}
			if !store.Found {
				it.Disabled = "pnpm could not be found from a non-interactive shell"
			}
			s.items = append(s.items, it)
		}

		if u, err := s.src.WSL.Used(ctx, d.Name, "/"); err != nil {
			s.fail(d.Name+" disk usage", err)
		} else {
			used = u
		}
	}

	size, ok := s.src.Stat(d.VhdFile)
	if d.VhdFile == "" || !ok {
		s.report(d.Name, nil)
		return
	}
	it := s.disk("compact:"+d.Name, d.Name, d.Name, d.VhdFile, size, used)
	it.Distro = d.Name
	if !d.Running {
		it.Detail = d.Name + " is stopped, so it's started for the trim"
	}
	s.disks = append(s.disks, it)
	s.report(d.Name, nil)
}

// disk builds a compaction item. Windows only ever returns whole unused
// blocks of the file, so the gap between its size and its use is not a
// promise: Frees is always 0, and only the run itself learns what came back.
// used is -1 when it could not be measured.
func (s *scanner) disk(id, title, disk, path string, size, used int64) plan.Item {
	it := plan.Item{
		ID: id, Section: sectionDisks, Kind: plan.KindCompact, Title: title,
		Size: size, Disk: disk, Cost: compactCost, Path: path,
		Detail: "in use unknown",
	}
	if used >= 0 {
		it.Detail = plan.Human(used) + " in use"
	}
	if !s.src.Facts.Elevated() {
		it.Disabled = "needs administrator rights"
	}
	return it
}

func (s *scanner) dockerDesktop(ctx context.Context, distros []wsl.Distro) {
	if s.src.Docker == nil {
		s.items = append(s.items, plan.Item{
			ID: "docker:missing", Section: sectionDocker, Kind: plan.KindNote,
			Title:  "Docker was not found",
			Detail: "docker.exe is not on the PATH or in Docker Desktop's install folder.",
		})
		s.report(sectionDocker, nil)
		return
	}
	// Docker's disk can only be trimmed while Docker runs, so nothing from
	// Docker is offered otherwise.
	if !s.src.Docker.Running(ctx) {
		s.items = append(s.items, plan.Item{
			ID: "docker:stopped", Section: sectionDocker, Kind: plan.KindNote,
			Title:  "Docker Desktop is not running",
			Detail: "Start it and run unbloat again to include its build cache, images and disk.",
		})
		s.report(sectionDocker, nil)
		return
	}
	s.docker = true

	if df, err := s.src.Docker.SystemDF(ctx); err != nil {
		s.fail("Docker usage", err)
	} else {
		if bc := df["Build Cache"]; bc.Size > 0 {
			s.items = append(s.items, plan.Item{
				ID: "docker:buildcache", Section: sectionDocker, Kind: plan.KindDockerBuildCache,
				Title: "Build cache", Size: bc.Size, Frees: bc.Size, Lands: plan.InsideDisk, Disk: "docker",
				Cost: "the next build is slower", Selected: true,
			})
		}
		if im := df["Images"]; im.Reclaimable > 0 {
			s.items = append(s.items, plan.Item{
				ID: "docker:images", Section: sectionDocker, Kind: plan.KindDockerImages,
				Title: "Images no container uses", Size: im.Reclaimable, Frees: im.Reclaimable,
				Lands: plan.InsideDisk, Disk: "docker", Cost: "downloaded again when needed",
			})
		}
		if vols, err := s.src.Docker.Volumes(ctx); err != nil {
			s.fail("Docker volumes", err)
		} else if len(vols) > 0 {
			s.items = append(s.items, volumesNote(vols, df["Local Volumes"].Size))
		}
	}

	base := ""
	for _, d := range distros {
		if d.Name == docker.DistroName {
			base = d.BasePath
		}
	}
	exists := func(p string) bool { _, ok := s.src.Stat(p); return ok }
	if path := docker.DataDisk(base, s.src.LocalAppData, exists); path != "" {
		size, _ := s.src.Stat(path)
		used, err := s.src.WSL.Used(ctx, docker.DistroName, docker.DataMount)
		if err != nil {
			s.fail("Docker disk usage", err)
			used = -1
		}
		it := s.disk("compact:docker", "Docker", "docker", path, size, used)
		it.Distro, it.Mount = docker.DistroName, docker.DataMount
		s.disks = append(s.disks, it)
	}
	s.report(sectionDocker, nil)
}

// volumesNote says what is being kept. It is a note: no volume is ever
// offered, named or anonymous.
func volumesNote(vols []docker.Volume, size int64) plan.Item {
	var named []string
	anonymous := 0
	for _, v := range vols {
		if v.Anonymous {
			anonymous++
		} else {
			named = append(named, v.Name)
		}
	}
	var parts []string
	switch len(named) {
	case 0:
	case 1:
		parts = append(parts, "one named, "+named[0])
	default:
		parts = append(parts, fmt.Sprintf("%d named, such as %s", len(named), named[0]))
	}
	switch anonymous {
	case 0:
	case 1:
		parts = append(parts, "one anonymous, which may hold database data")
	default:
		parts = append(parts, fmt.Sprintf("%d anonymous, which may hold database data", anonymous))
	}
	return plan.Item{
		ID: "docker:volumes", Section: sectionDocker, Kind: plan.KindNote,
		Title: "Volumes are kept", Size: size, Detail: strings.Join(parts, "; "),
	}
}
