package plan

import (
	"sort"
	"strings"
)

// Phase fixes when a step runs. Lower runs first.
type Phase int

const (
	PhaseWindowsCaches Phase = iota
	PhaseWSLCaches
	PhaseDocker
	PhaseTrim   // needs the distros, and Docker, still running
	PhaseStop   // Docker Desktop stops, then WSL shuts down
	PhaseUnlock // every disk to compact must be free
	PhaseCompact
)

// Step is one thing the run does.
type Step struct {
	ID        string
	Title     string
	Phase     Phase
	Item      Item
	DependsOn []string // every one must have finished
	AnyOf     []string // at least one must have finished
	Critical  bool     // runs to completion even after an interrupt
}

// Steps turns the selection into the order it runs in. The order is fixed:
// selecting items in a different order changes nothing.
//
// A compaction brings 3 more steps with it. Its disk is trimmed while
// everything still runs, because a compaction without a trim returns almost
// nothing. One stop step follows, which runs only if at least one trim worked,
// since shutting WSL down for no compaction costs the user their windows for
// nothing. Then each disk is checked for locks, and compacted only if its own
// trim and its own check both succeeded.
func (p Plan) Steps() []Step {
	var steps []Step
	var disks []Item
	for _, it := range p.Selected() {
		switch it.Kind {
		case KindWindowsTemp, KindWindowsRemove, KindWindowsTool:
			steps = append(steps, Step{ID: it.ID, Title: it.Title, Phase: PhaseWindowsCaches, Item: it})
		case KindWSLRemove, KindWSLPnpmPrune:
			steps = append(steps, Step{ID: it.ID, Title: it.Title, Phase: PhaseWSLCaches, Item: it})
		case KindDockerBuildCache, KindDockerImages:
			steps = append(steps, Step{ID: it.ID, Title: it.Title, Phase: PhaseDocker, Item: it})
		case KindCompact:
			disks = append(disks, it)
		}
	}

	if len(disks) > 0 {
		var trims []string
		for _, d := range disks {
			trims = append(trims, "trim:"+d.Disk)
			steps = append(steps, Step{ID: "trim:" + d.Disk, Title: "Trim " + d.Title, Phase: PhaseTrim, Item: d})
		}
		steps = append(steps, Step{ID: "stop", Title: "Stop Docker Desktop and shut down WSL", Phase: PhaseStop, AnyOf: trims})
		for _, d := range disks {
			steps = append(steps,
				Step{ID: "unlock:" + d.Disk, Title: "Check " + d.Title + " is not in use", Phase: PhaseUnlock, Item: d, DependsOn: []string{"stop"}},
				Step{ID: "compact:" + d.Disk, Title: "Compact " + d.Title, Phase: PhaseCompact, Item: d,
					DependsOn: []string{"trim:" + d.Disk, "unlock:" + d.Disk}, Critical: true},
			)
		}
	}

	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Phase < steps[j].Phase })
	return steps
}

// GateFacts is what the shutdown gate looks at.
type GateFacts struct {
	Ancestors      []string
	OnBattery      bool
	BatteryKnown   bool
	BatteryPercent int
}

// Gate returns every reason the shutdown must not happen. An empty result
// means it may. It is checked before a run that compacts anything; a refusal
// leaves the user free to untick the compactions and run the rest.
func Gate(f GateFacts) []string {
	var reasons []string
	for _, name := range f.Ancestors {
		if n := strings.ToLower(name); n == "wsl.exe" || n == "wslhost.exe" {
			reasons = append(reasons, "unbloat was started from inside WSL. Shutting WSL down would stop unbloat partway through. Start it from PowerShell or Windows Terminal instead.")
			break
		}
	}
	if f.OnBattery && (!f.BatteryKnown || f.BatteryPercent < 50) {
		reasons = append(reasons, "the machine is on battery and below 50%. Losing power while a disk is being compacted can corrupt it. Plug in first.")
	}
	return reasons
}
