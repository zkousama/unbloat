// Package plan is what unbloat will do: the items a scan found, which of them
// are selected, the order they run in, and the rules that stop a run. It knows
// nothing about Windows or the terminal.
package plan

import "fmt"

// Kind is what an item does.
type Kind int

const (
	KindNote             Kind = iota // information only, never selectable
	KindWSLRemove                    // delete a cache directory inside a distro
	KindWSLPnpmPrune                 // pnpm store prune inside a distro
	KindDockerBuildCache             // docker builder prune
	KindDockerImages                 // docker image prune
	KindWindowsTemp                  // old files in %TEMP%
	KindWindowsRemove                // delete a cache directory on Windows
	KindWindowsTool                  // a package manager's own clean command
	KindCompact                      // trim, stop, check and compact one disk
)

// Lands is where an item's freed space ends up.
type Lands int

const (
	OnC        Lands = iota // on the drive at once
	InsideDisk              // inside a virtual disk, until it is compacted
)

// Item is one row of the checklist.
type Item struct {
	ID       string
	Section  string
	Kind     Kind
	Title    string
	Detail   string
	Size     int64
	Frees    int64
	Lands    Lands
	Disk     string
	Cost     string
	Selected bool
	Disabled string

	Distro string
	Path   string
	Mount  string
	Tool   string
}

// Plan is everything a scan found.
type Plan struct {
	Items         []Item
	Running       []string // distros running when the scan ran
	DockerRunning bool
}

// Toggle selects or deselects an item. Notes and disabled items refuse, with
// the reason.
func (p *Plan) Toggle(id string) error {
	for i := range p.Items {
		it := &p.Items[i]
		if it.ID != id {
			continue
		}
		if it.Kind == KindNote {
			return fmt.Errorf("%s is information, not something to run", it.Title)
		}
		if it.Disabled != "" {
			return fmt.Errorf("%s: %s", it.Title, it.Disabled)
		}
		it.Selected = !it.Selected
		return nil
	}
	return fmt.Errorf("there is no item %q", id)
}

// Totals is the selected space, split by where it lands.
type Totals struct {
	OnC         int64 // reaches the drive when the run finishes
	InsideDisks int64 // freed inside virtual disks that are not being compacted
}

// Totals never adds space inside a virtual disk to space on C: unless that
// disk is being compacted, because until then the drive does not change.
func (p Plan) Totals() Totals {
	compacting := map[string]bool{}
	for _, it := range p.Items {
		if it.Kind == KindCompact && it.Selected {
			compacting[it.Disk] = true
		}
	}
	var t Totals
	for _, it := range p.Items {
		if !it.Selected || it.Kind == KindNote {
			continue
		}
		switch {
		case it.Kind == KindCompact, it.Lands == OnC, compacting[it.Disk]:
			t.OnC += it.Frees
		default:
			t.InsideDisks += it.Frees
		}
	}
	return t
}

// Compacting reports whether any disk is selected for compaction.
func (p Plan) Compacting() bool {
	for _, it := range p.Items {
		if it.Kind == KindCompact && it.Selected {
			return true
		}
	}
	return false
}

// Selected lists the selected items in order.
func (p Plan) Selected() []Item {
	var out []Item
	for _, it := range p.Items {
		if it.Selected && it.Kind != KindNote {
			out = append(out, it)
		}
	}
	return out
}

// Human formats bytes the way Windows Explorer does: powers of 1024 labelled
// GB, MB and KB. Users compare these numbers against Explorer.
func Human(n int64) string {
	const kb, mb, gb = 1 << 10, 1 << 20, 1 << 30
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.0f MB", float64(n)/mb)
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
