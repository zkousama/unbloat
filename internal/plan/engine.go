package plan

import (
	"context"
	"fmt"
)

// Status is how a step ended.
type Status int

const (
	Done Status = iota + 1
	Failed
	Skipped
)

func (s Status) String() string {
	switch s {
	case Done:
		return "done"
	case Failed:
		return "failed"
	case Skipped:
		return "skipped"
	}
	return "pending"
}

// Outcome is what happened to one step.
type Outcome struct {
	Step   Step
	Status Status
	Freed  int64
	Err    error
	Reason string // why a step was skipped
}

// Executor carries out a step.
type Executor interface {
	Execute(ctx context.Context, s Step) (freed int64, err error)
}

// Hooks let the interface follow a run.
type Hooks struct {
	Started  func(Step)
	Finished func(Outcome)
}

// Run executes steps in order. A failed step does not stop the run: steps that
// do not depend on it carry on, and steps that do are skipped with a reason
// naming it. An interrupt, meaning ctx ending, stops the run before the next
// step starts. Every step gets a context that ignores the interrupt, so a step
// that has started always finishes, and a compaction is never cut off partway.
func Run(ctx context.Context, steps []Step, ex Executor, h Hooks) []Outcome {
	status := map[string]Status{}
	titles := map[string]string{}
	outcomes := make([]Outcome, 0, len(steps))

	for _, s := range steps {
		titles[s.ID] = s.Title
		if h.Started != nil {
			h.Started(s)
		}
		o := Outcome{Step: s}
		switch {
		case ctx.Err() != nil:
			o.Status, o.Reason = Skipped, "stopped before this step started"
		case unmet(s, status, titles) != "":
			o.Status, o.Reason = Skipped, unmet(s, status, titles)
		default:
			freed, err := ex.Execute(context.WithoutCancel(ctx), s)
			if err != nil {
				o.Status, o.Err = Failed, err
			} else {
				o.Status, o.Freed = Done, freed
			}
		}
		status[s.ID] = o.Status
		outcomes = append(outcomes, o)
		if h.Finished != nil {
			h.Finished(o)
		}
	}
	return outcomes
}

func unmet(s Step, status map[string]Status, titles map[string]string) string {
	for _, id := range s.DependsOn {
		if status[id] != Done {
			return fmt.Sprintf("skipped because %q did not finish", titles[id])
		}
	}
	if len(s.AnyOf) == 0 {
		return ""
	}
	for _, id := range s.AnyOf {
		if status[id] == Done {
			return ""
		}
	}
	return "skipped because no disk was trimmed, so there is nothing to compact"
}
