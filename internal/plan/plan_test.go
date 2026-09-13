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

func TestCompactingADiskMovesItsShareOntoC(t *testing.T) {
	p := sample()
	if err := p.Toggle("compact:Ubuntu"); err != nil {
		t.Fatal(err)
	}
	got := p.Totals()
	// Windows temp 1, Ubuntu's own slack 14, and the 2 freed inside Ubuntu.
	if got.OnC != 17*gb {
		t.Fatalf("on C: %s", Human(got.OnC))
	}
	// Docker is still not being compacted, so its 12 stays inside.
	if got.InsideDisks != 12*gb {
		t.Fatalf("inside disks: %s", Human(got.InsideDisks))
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
