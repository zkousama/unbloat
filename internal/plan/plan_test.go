package plan

import (
	"strings"
	"testing"
)

const gb = int64(1) << 30

func sample() Plan {
	return Plan{Items: []Item{
		{ID: "cache:Ubuntu:npm", Kind: KindWSLRemove, Frees: 2 * gb, Lands: InsideDisk, Disk: "Ubuntu", Selected: true},
		{ID: "docker:buildcache", Kind: KindDockerBuildCache, Frees: 12 * gb, Lands: InsideDisk, Disk: "docker", Selected: true},
		{ID: "docker:volumes", Kind: KindNote, Frees: 4 * gb},
		{ID: "win:temp", Kind: KindWindowsTemp, Frees: 1 * gb, Lands: OnC, Selected: true},
		{ID: "compact:Ubuntu", Kind: KindCompact, Frees: 14 * gb, Disk: "Ubuntu"},
		{ID: "compact:docker", Kind: KindCompact, Frees: 11 * gb, Disk: "docker", Disabled: "needs administrator rights"},
	}}
}

// The mistake this model exists to prevent: 15 GB freed inside the disks and C:
// did not move, because nothing was compacted.
func TestTotalsKeepSpaceInsideDisksApartFromSpaceOnC(t *testing.T) {
	got := sample().Totals()
	if got.OnC != 1*gb || got.InsideDisks != 14*gb {
		t.Fatalf("got %+v", got)
	}
}

// The mistake this test exists to prevent: a compaction's Frees is a guess,
// and guesses never move space onto C:.
func TestCompactingADiskNeverAddsToTheTotalOnC(t *testing.T) {
	p := sample()
	before := p.Totals()
	if err := p.Toggle("compact:Ubuntu"); err != nil {
		t.Fatal(err)
	}
	got := p.Totals()
	if got.OnC != before.OnC {
		t.Fatalf("on C: %s, want unchanged at %s", Human(got.OnC), Human(before.OnC))
	}
	if got.InsideDisks != before.InsideDisks {
		t.Fatalf("inside disks: %s, want unchanged at %s", Human(got.InsideDisks), Human(before.InsideDisks))
	}
}

func TestADisabledItemCannotBeSelected(t *testing.T) {
	p := sample()
	err := p.Toggle("compact:docker")
	if err == nil || !strings.Contains(err.Error(), "needs administrator rights") {
		t.Fatalf("err = %v", err)
	}
	if p.Items[5].Selected {
		t.Fatal("a disabled item was selected")
	}
}

func TestANoteCannotBeSelected(t *testing.T) {
	p := sample()
	if err := p.Toggle("docker:volumes"); err == nil {
		t.Fatal("a note must not be selectable")
	}
	if p.Totals().OnC != 1*gb {
		t.Fatal("a note must never count towards a total")
	}
}

func TestToggleUnknownItem(t *testing.T) {
	p := sample()
	if err := p.Toggle("nope"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSelectedAndCompacting(t *testing.T) {
	p := sample()
	if p.Compacting() {
		t.Fatal("nothing is compacting yet")
	}
	if len(p.Selected()) != 3 {
		t.Fatalf("selected = %d", len(p.Selected()))
	}
	_ = p.Toggle("compact:Ubuntu")
	if !p.Compacting() {
		t.Fatal("Ubuntu is compacting")
	}
}

// Toggle refuses a disabled item, but an item can arrive already ticked. It
// still never runs.
func TestADisabledItemNeverRunsEvenWhenSelected(t *testing.T) {
	p := Plan{Items: []Item{
		{ID: "win:temp", Kind: KindWindowsTemp, Selected: true, Disabled: "x"},
		{ID: "compact:Ubuntu", Kind: KindCompact, Disk: "Ubuntu", Selected: true, Disabled: "x"},
	}}
	if got := p.Selected(); len(got) != 0 {
		t.Errorf("selected = %v", got)
	}
	if steps := p.Steps(); len(steps) != 0 {
		t.Errorf("steps = %v", ids(steps))
	}
	if p.Compacting() {
		t.Error("a disabled compaction counts as compacting")
	}
}

// Powers of 1024 labelled GB, as Windows Explorer shows them.
func TestHuman(t *testing.T) {
	for n, want := range map[int64]string{
		0:              "0 B",
		512:            "512 B",
		2 << 10:        "2 KB",
		387 << 20:      "387 MB",
		40_050_581_094: "37.3 GB",
	} {
		if got := Human(n); got != want {
			t.Errorf("Human(%d) = %q, want %q", n, got, want)
		}
	}
}
