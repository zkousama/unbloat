package plan

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeExecutor struct {
	fail  map[string]bool
	ran   []string
	onRun func(ctx context.Context, s Step)
}

func (f *fakeExecutor) Execute(ctx context.Context, s Step) (int64, error) {
	f.ran = append(f.ran, s.ID)
	if f.onRun != nil {
		f.onRun(ctx, s)
	}
	if f.fail[s.ID] {
		return 0, errors.New("boom")
	}
	return 10, nil
}

func twoDisks() []Step {
	return Plan{Items: []Item{
		{ID: "win:temp", Kind: KindWindowsTemp, Title: "%TEMP%", Selected: true},
		{ID: "compact:Ubuntu", Kind: KindCompact, Disk: "Ubuntu", Title: "Ubuntu", Selected: true},
		{ID: "compact:docker", Kind: KindCompact, Disk: "docker", Title: "Docker", Selected: true},
	}}.Steps()
}

func outcome(outs []Outcome, id string) Outcome {
	for _, o := range outs {
		if o.Step.ID == id {
			return o
		}
	}
	return Outcome{}
}

func TestAFailedTrimSkipsOnlyItsOwnCompaction(t *testing.T) {
	ex := &fakeExecutor{fail: map[string]bool{"trim:docker": true}}
	outs := Run(context.Background(), twoDisks(), ex, Hooks{})

	if o := outcome(outs, "compact:docker"); o.Status != Skipped || !strings.Contains(o.Reason, "Trim Docker") {
		t.Errorf("compact:docker = %v %q", o.Status, o.Reason)
	}
	if o := outcome(outs, "compact:Ubuntu"); o.Status != Done {
		t.Errorf("compact:Ubuntu = %v", o.Status)
	}
	for _, id := range ex.ran {
		if id == "compact:docker" {
			t.Fatal("a disk whose trim failed was compacted")
		}
	}
}

// WSL is never shut down for nothing.
func TestNoShutdownWhenEveryTrimFailed(t *testing.T) {
	ex := &fakeExecutor{fail: map[string]bool{"trim:Ubuntu": true, "trim:docker": true}}
	outs := Run(context.Background(), twoDisks(), ex, Hooks{})

	if o := outcome(outs, "stop"); o.Status != Skipped {
		t.Fatalf("stop = %v", o.Status)
	}
	for _, id := range ex.ran {
		if id == "stop" || strings.HasPrefix(id, "unlock:") || strings.HasPrefix(id, "compact:") {
			t.Fatalf("%s ran after every trim failed", id)
		}
	}
}

func TestALockedDiskIsNotCompacted(t *testing.T) {
	ex := &fakeExecutor{fail: map[string]bool{"unlock:Ubuntu": true}}
	outs := Run(context.Background(), twoDisks(), ex, Hooks{})
	if o := outcome(outs, "compact:Ubuntu"); o.Status != Skipped {
		t.Fatalf("compact:Ubuntu = %v", o.Status)
	}
	if o := outcome(outs, "compact:docker"); o.Status != Done {
		t.Fatalf("compact:docker = %v", o.Status)
	}
}

func TestAnInterruptStopsBeforeTheNextStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ex := &fakeExecutor{onRun: func(_ context.Context, s Step) {
		if s.ID == "win:temp" {
			cancel()
		}
	}}
	outs := Run(ctx, twoDisks(), ex, Hooks{})

	if len(ex.ran) != 1 {
		t.Fatalf("ran %v after the interrupt", ex.ran)
	}
	for _, o := range outs[1:] {
		if o.Status != Skipped || !strings.Contains(o.Reason, "stopped") {
			t.Errorf("%s = %v %q", o.Step.ID, o.Status, o.Reason)
		}
	}
}

// Ctrl+C during a compaction must not cancel it.
func TestACriticalStepIgnoresAnInterruptWhileItRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stillLive := false
	steps := []Step{{ID: "compact:Ubuntu", Title: "Compact Ubuntu", Phase: PhaseCompact, Critical: true}}
	ex := &fakeExecutor{onRun: func(stepCtx context.Context, s Step) {
		cancel()
		stillLive = stepCtx.Err() == nil
	}}
	outs := Run(ctx, steps, ex, Hooks{})
	if !stillLive {
		t.Fatal("the compaction's context was cancelled")
	}
	if outs[0].Status != Done {
		t.Fatalf("status = %v", outs[0].Status)
	}
}

// Any step cut off partway can leave things half done, so every step that has
// started finishes. The interrupt still skips the steps after it.
func TestAnyStepIgnoresAnInterruptWhileItRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stillLive := false
	steps := []Step{
		{ID: "cache:Ubuntu:npm", Title: "npm cache", Phase: PhaseWSLCaches},
		{ID: "docker:buildcache", Title: "Build cache", Phase: PhaseDocker},
	}
	ex := &fakeExecutor{onRun: func(stepCtx context.Context, s Step) {
		if s.ID == "cache:Ubuntu:npm" {
			cancel()
			stillLive = stepCtx.Err() == nil
		}
	}}
	outs := Run(ctx, steps, ex, Hooks{})
	if !stillLive {
		t.Fatal("the running step's context was cancelled")
	}
	if outs[0].Status != Done {
		t.Errorf("status = %v", outs[0].Status)
	}
	if outs[1].Status != Skipped || !strings.Contains(outs[1].Reason, "stopped before this step started") {
		t.Errorf("next step = %v %q", outs[1].Status, outs[1].Reason)
	}
}

func TestHooksSeeEveryStep(t *testing.T) {
	started, finished := 0, 0
	outs := Run(context.Background(), twoDisks(), &fakeExecutor{}, Hooks{
		Started:  func(Step) { started++ },
		Finished: func(Outcome) { finished++ },
	})
	if finished != len(outs) {
		t.Errorf("finished %d of %d", finished, len(outs))
	}
	if started != len(outs) {
		t.Errorf("started %d of %d; skipped steps are reported as started too", started, len(outs))
	}
}
