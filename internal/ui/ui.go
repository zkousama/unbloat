// Package ui is the terminal interface. It knows nothing about Windows: every
// fact and every action reaches it through Deps.
package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/scan"
	"github.com/zkousama/unbloat/internal/sys"
)

// Deps is everything the interface needs from the rest of unbloat.
type Deps struct {
	Facts    sys.Facts
	Scan     func(ctx context.Context, report scan.Report) plan.Plan
	Executor plan.Executor
	Describe func(plan.Step) []string
	Drive    string // the drive reported in the header and summary, such as C:\
	LogPath  string
}

type screen int

const (
	screenElevate screen = iota
	screenScan
	screenChecklist
	screenConfirm
	screenGate
	screenRun
	screenSummary
)

// Messages from the scan and the run, which happen in the background.
type (
	scanProgress struct {
		target string
		err    error
	}
	scanDone     struct{ plan plan.Plan }
	stepStarted  struct{ step plan.Step }
	stepFinished struct{ outcome plan.Outcome }
	runDone      struct{ outcomes []plan.Outcome }
)

type target struct {
	name string
	err  error
}

// Model is the whole interface state.
type Model struct {
	deps   Deps
	ctx    context.Context
	cancel context.CancelFunc
	events chan tea.Msg

	screen  screen
	height  int
	message string

	freeBefore, freeAfter, total int64

	targets      []target
	plan         plan.Plan
	cursor       int // index into rows()
	showCommands bool
	steps        []plan.Step
	gate         []string

	current     *plan.Step
	outcomes    []plan.Outcome
	interrupted bool
	relaunched  bool
}

func New(d Deps) Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := Model{deps: d, ctx: ctx, cancel: cancel, events: make(chan tea.Msg, 64), screen: screenScan}
	m.freeBefore, m.total, _ = d.Facts.Space(d.Drive)
	if !d.Facts.Elevated() {
		m.screen = screenElevate
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.screen == screenScan {
		return m.scan()
	}
	return nil
}

// Relaunched reports whether an elevated copy was started and this one quit.
func (m Model) Relaunched() bool { return m.relaunched }

// Finished reports whether the run reached the summary.
func (m Model) Finished() bool { return m.screen == screenSummary }

func listen(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

// scan starts the scan in the background. Each part it finishes arrives as a
// message.
func (m Model) scan() tea.Cmd {
	events, ctx, scanFn := m.events, m.ctx, m.deps.Scan
	go func() {
		p := scanFn(ctx, func(name string, err error) { events <- scanProgress{name, err} })
		events <- scanDone{p}
	}()
	return listen(events)
}

// run starts the steps in the background.
func (m Model) run() tea.Cmd {
	events, ctx, steps, ex := m.events, m.ctx, m.steps, m.deps.Executor
	go func() {
		outcomes := plan.Run(ctx, steps, ex, plan.Hooks{
			Started:  func(s plan.Step) { events <- stepStarted{s} },
			Finished: func(o plan.Outcome) { events <- stepFinished{o} },
		})
		events <- runDone{outcomes}
	}()
	return listen(events)
}

// rows are the checklist items the cursor can land on: everything but notes.
func (m Model) rows() []int {
	var rows []int
	for i, it := range m.plan.Items {
		if it.Kind != plan.KindNote {
			rows = append(rows, i)
		}
	}
	return rows
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case scanProgress:
		m.targets = append(m.targets, target{msg.target, msg.err})
		return m, listen(m.events)
	case scanDone:
		m.plan, m.cursor, m.screen = msg.plan, 0, screenChecklist
	case stepStarted:
		s := msg.step
		m.current = &s
		return m, listen(m.events)
	case stepFinished:
		m.outcomes = append(m.outcomes, msg.outcome)
		m.current = nil
		return m, listen(m.events)
	case runDone:
		m.outcomes, m.current, m.screen = msg.outcomes, nil, screenSummary
		m.freeAfter, _, _ = m.deps.Facts.Space(m.deps.Drive)
	case tea.KeyMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	return m, tea.Quit
}

func (m Model) key(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		if m.screen != screenRun {
			return m.quit()
		}
		// The engine stops before the next step and lets a compaction finish.
		if !m.interrupted {
			m.interrupted = true
			m.cancel()
		}
		return m, nil
	}
	m.message = ""

	switch m.screen {
	case screenElevate:
		switch key {
		case "y":
			if err := m.deps.Facts.RelaunchElevated(); err != nil {
				m.message = "Could not start as administrator (" + err.Error() + "). Continuing without compaction."
				m.screen = screenScan
				return m, m.scan()
			}
			m.relaunched = true
			return m.quit()
		case "n", "enter":
			m.screen = screenScan
			return m, m.scan()
		case "q":
			return m.quit()
		}

	case screenChecklist:
		rows := m.rows()
		switch key {
		case "up", "k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "j":
			m.cursor = max(min(m.cursor+1, len(rows)-1), 0)
		case " ", "space", "x":
			if len(rows) > 0 {
				if err := m.plan.Toggle(m.plan.Items[rows[m.cursor]].ID); err != nil {
					m.message = err.Error()
				}
			}
		case "enter", "c":
			if len(m.plan.Selected()) == 0 {
				m.message = "Nothing is ticked."
				break
			}
			m.steps = m.plan.Steps()
			m.showCommands = key == "c"
			m.screen = screenConfirm
		case "q":
			return m.quit()
		}

	case screenConfirm:
		switch key {
		case "c":
			m.showCommands = !m.showCommands
		case "esc":
			m.screen = screenChecklist
		case "enter":
			if !m.plan.Compacting() {
				m.screen = screenRun
				return m, m.run()
			}
			power := m.deps.Facts.Power()
			m.gate = plan.Gate(plan.GateFacts{
				Ancestors:      m.deps.Facts.Ancestors(),
				OnBattery:      power.OnBattery,
				BatteryKnown:   power.Known,
				BatteryPercent: power.Percent,
			})
			m.screen = screenGate
		case "q":
			return m.quit()
		}

	case screenGate:
		switch key {
		case "esc":
			m.screen = screenChecklist
		case "enter":
			if len(m.gate) == 0 {
				m.screen = screenRun
				return m, m.run()
			}
		case "q":
			return m.quit()
		}

	case screenSummary:
		switch key {
		case "q", "enter", "esc":
			return m.quit()
		}
	}
	return m, nil
}
