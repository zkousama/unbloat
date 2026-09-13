package plan

import (
	"reflect"
	"strings"
	"testing"
)

func ids(steps []Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func step(steps []Step, id string) Step {
	for _, s := range steps {
		if s.ID == id {
			return s
		}
	}
	return Step{}
}

// The user chooses what runs, never when. The items here are listed in the
// wrong order and still come out in the fixed order.
func TestStepsRunInTheFixedOrderWhateverTheSelectionOrder(t *testing.T) {
	p := Plan{Items: []Item{
		{ID: "compact:Ubuntu", Kind: KindCompact, Disk: "Ubuntu", Title: "Ubuntu", Selected: true},
		{ID: "docker:buildcache", Kind: KindDockerBuildCache, Title: "build cache", Selected: true},
		{ID: "cache:Ubuntu:npm", Kind: KindWSLRemove, Title: "npm cache", Selected: true},
		{ID: "win:temp", Kind: KindWindowsTemp, Title: "%TEMP%", Selected: true},
	}}
	want := []string{"win:temp", "cache:Ubuntu:npm", "docker:buildcache", "trim:Ubuntu", "stop", "unlock:Ubuntu", "compact:Ubuntu"}
	if got := ids(p.Steps()); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

// A compaction depends on its own trim and its own unlock check, never on
// another disk's.
func TestCompactionDependsOnItsTrimAndUnlock(t *testing.T) {
	p := Plan{Items: []Item{
		{ID: "compact:Ubuntu", Kind: KindCompact, Disk: "Ubuntu", Title: "Ubuntu", Selected: true},
		{ID: "compact:docker", Kind: KindCompact, Disk: "docker", Title: "Docker", Selected: true},
	}}
	steps := p.Steps()

	c := step(steps, "compact:docker")
	if !reflect.DeepEqual(c.DependsOn, []string{"trim:docker", "unlock:docker"}) {
		t.Errorf("compact:docker depends on %v", c.DependsOn)
	}
	if !c.Critical {
		t.Error("a compaction must be critical, so an interrupt cannot stop it partway")
	}
	if got := step(steps, "unlock:Ubuntu").DependsOn; !reflect.DeepEqual(got, []string{"stop"}) {
		t.Errorf("unlock:Ubuntu depends on %v", got)
	}
	if got := step(steps, "stop").AnyOf; !reflect.DeepEqual(got, []string{"trim:Ubuntu", "trim:docker"}) {
		t.Errorf("stop runs after any of %v", got)
	}
}

func TestNoShutdownWithoutACompaction(t *testing.T) {
	p := Plan{Items: []Item{{ID: "win:temp", Kind: KindWindowsTemp, Selected: true}}}
	for _, s := range p.Steps() {
		if s.Phase >= PhaseTrim {
			t.Fatalf("got step %s with nothing to compact", s.ID)
		}
	}
}

func TestUnselectedItemsAndNotesProduceNoSteps(t *testing.T) {
	p := Plan{Items: []Item{
		{ID: "docker:volumes", Kind: KindNote, Selected: true},
		{ID: "docker:images", Kind: KindDockerImages},
	}}
	if steps := p.Steps(); len(steps) != 0 {
		t.Fatalf("got %v", ids(steps))
	}
}

// Recorded on the machine this was designed against: the ancestry of a Windows
// process started from a WSL shell, and of one started from Windows Terminal.
var (
	fromWSL      = []string{"wsl.exe", "wsl.exe", "ubuntu.exe", "WindowsTerminal.exe", "explorer.exe"}
	fromTerminal = []string{"powershell.exe", "WindowsTerminal.exe", "explorer.exe"}
)

func TestGateRefusesWhenLaunchedFromInsideWSL(t *testing.T) {
	reasons := Gate(GateFacts{Ancestors: fromWSL})
	if len(reasons) != 1 || !strings.Contains(reasons[0], "inside WSL") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestGateAllowsWindowsTerminalOnMains(t *testing.T) {
	if reasons := Gate(GateFacts{Ancestors: fromTerminal}); len(reasons) != 0 {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestGateBattery(t *testing.T) {
	for name, tc := range map[string]struct {
		f      GateFacts
		refuse bool
	}{
		"mains":            {GateFacts{OnBattery: false, BatteryKnown: true, BatteryPercent: 10}, false},
		"battery at 80":    {GateFacts{OnBattery: true, BatteryKnown: true, BatteryPercent: 80}, false},
		"battery at 50":    {GateFacts{OnBattery: true, BatteryKnown: true, BatteryPercent: 50}, false},
		"battery at 49":    {GateFacts{OnBattery: true, BatteryKnown: true, BatteryPercent: 49}, true},
		"battery, unknown": {GateFacts{OnBattery: true, BatteryKnown: false}, true},
	} {
		tc.f.Ancestors = fromTerminal
		if got := len(Gate(tc.f)) > 0; got != tc.refuse {
			t.Errorf("%s: refused %v, want %v", name, got, tc.refuse)
		}
	}
}
