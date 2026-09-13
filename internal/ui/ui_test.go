package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/scan"
	"github.com/zkousama/unbloat/internal/sys"
)

var (
	enter  = tea.KeyMsg{Type: tea.KeyEnter}
	space  = tea.KeyMsg{Type: tea.KeySpace}
	esc    = tea.KeyMsg{Type: tea.KeyEsc}
	down   = tea.KeyMsg{Type: tea.KeyDown}
	ctrlC  = tea.KeyMsg{Type: tea.KeyCtrlC}
	letter = func(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
)

type recordingExecutor struct{ ran []string }

func (r *recordingExecutor) Execute(_ context.Context, s plan.Step) (int64, error) {
	r.ran = append(r.ran, s.ID)
	return 1 << 30, nil
}

func samplePlan() plan.Plan {
	return plan.Plan{
		Running:       []string{"Ubuntu"},
		DockerRunning: true,
		Items: []plan.Item{
			{ID: "win:temp", Section: "Caches on Windows", Kind: plan.KindWindowsTemp, Title: "%TEMP%", Size: 1 << 30, Frees: 1 << 30, Lands: plan.OnC, Selected: true},
			{ID: "docker:volumes", Section: "Docker", Kind: plan.KindNote, Title: "Volumes are kept", Detail: "2 named, such as shop_postgres-data"},
			{ID: "docker:buildcache", Section: "Docker", Kind: plan.KindDockerBuildCache, Title: "Build cache", Size: 12 << 30, Frees: 12 << 30, Lands: plan.InsideDisk, Disk: "docker", Selected: true},
			{ID: "compact:Ubuntu", Section: "Disks", Kind: plan.KindCompact, Title: "Ubuntu", Disk: "Ubuntu", Size: 37 << 30, Frees: 14 << 30},
			{ID: "compact:docker", Section: "Disks", Kind: plan.KindCompact, Title: "Docker", Disk: "docker", Size: 25 << 30, Frees: 11 << 30, Disabled: "needs administrator rights"},
		},
	}
}

func deps(facts *sys.FakeFacts, ex plan.Executor) Deps {
	return Deps{
		Facts:    facts,
		Scan:     func(context.Context, scan.Report) plan.Plan { return samplePlan() },
		Executor: ex,
		Describe: func(s plan.Step) []string { return []string{"run " + s.ID} },
		Drive:    `C:\`,
		LogPath:  `C:\Users\dev\AppData\Local\unbloat\logs\2026-09-13T04-00-00.log`,
	}
}

func press(t *testing.T, m Model, keys ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(k)
		m = next.(Model)
	}
	return m, cmd
}

// drain runs commands the way Bubble Tea would, feeding each message back in,
// until nothing is left to do or the program quits.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 1000 {
			t.Fatal("the program never settled")
		}
		msg := cmd()
		if _, quit := msg.(tea.QuitMsg); quit {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func checklist(t *testing.T, facts *sys.FakeFacts, ex plan.Executor) Model {
	t.Helper()
	m := New(deps(facts, ex))
	m = drain(t, m, m.Init())
	if m.screen != screenChecklist {
		t.Fatalf("screen = %v after the scan", m.screen)
	}
	return m
}

func TestNotElevatedAsksBeforeScanning(t *testing.T) {
	facts := &sys.FakeFacts{}
	m := New(deps(facts, &recordingExecutor{}))
	if m.screen != screenElevate || m.Init() != nil {
		t.Fatalf("screen = %v", m.screen)
	}
	m, cmd := press(t, m, letter('n'))
	if m.screen != screenScan || cmd == nil {
		t.Fatalf("declining should start the scan, screen = %v", m.screen)
	}
	if facts.Relaunched {
		t.Fatal("relaunched without being asked to")
	}
}

func TestAcceptingElevationRelaunchesAndQuits(t *testing.T) {
	facts := &sys.FakeFacts{}
	m, cmd := press(t, New(deps(facts, &recordingExecutor{})), letter('y'))
	if !facts.Relaunched || !m.Relaunched() {
		t.Fatal("did not relaunch")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("did not quit after relaunching")
	}
}

func TestCursorSkipsNotes(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m, _ = press(t, m, down)
	if got := m.plan.Items[m.rows()[m.cursor]].ID; got != "docker:buildcache" {
		t.Fatalf("cursor on %s", got)
	}
}

func TestTickingADisabledItemShowsWhy(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m, _ = press(t, m, down, down, down, space)
	if !strings.Contains(m.message, "needs administrator rights") {
		t.Fatalf("message = %q", m.message)
	}
	if m.plan.Items[4].Selected {
		t.Fatal("a disabled item was ticked")
	}
}

func TestNothingSelectedCannotGoForward(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m, _ = press(t, m, space, down, space, enter)
	if m.screen != screenChecklist || m.message == "" {
		t.Fatalf("screen %v, message %q", m.screen, m.message)
	}
}

func TestARunWithoutCompactionSkipsTheGate(t *testing.T) {
	ex := &recordingExecutor{}
	m := checklist(t, &sys.FakeFacts{IsElevated: true, Chain: []string{"wsl.exe"}}, ex)
	m, cmd := press(t, m, enter, enter)
	if m.screen != screenRun {
		t.Fatalf("screen = %v", m.screen)
	}
	m = drain(t, m, cmd)
	if !m.Finished() || strings.Join(ex.ran, ",") != "win:temp,docker:buildcache" {
		t.Fatalf("finished %v, ran %v", m.Finished(), ex.ran)
	}
}

func TestTheGateRefusesAndLetsTheUserGoBack(t *testing.T) {
	ex := &recordingExecutor{}
	facts := &sys.FakeFacts{IsElevated: true, Chain: []string{"wsl.exe", "WindowsTerminal.exe"}}
	m := checklist(t, facts, ex)
	m, _ = press(t, m, down, down, space) // tick Ubuntu's compaction
	m, _ = press(t, m, enter, enter)
	if m.screen != screenGate || len(m.gate) == 0 {
		t.Fatalf("screen %v, gate %v", m.screen, m.gate)
	}
	m, cmd := press(t, m, enter)
	if m.screen != screenGate || cmd != nil || len(ex.ran) != 0 {
		t.Fatal("a refused gate let the run start")
	}
	m, _ = press(t, m, esc)
	if m.screen != screenChecklist {
		t.Fatalf("esc went to %v", m.screen)
	}
}

func TestThePassedGateRunsTheCompaction(t *testing.T) {
	ex := &recordingExecutor{}
	facts := &sys.FakeFacts{IsElevated: true, Chain: []string{"powershell.exe", "WindowsTerminal.exe"}}
	m := checklist(t, facts, ex)
	m, _ = press(t, m, down, down, space, enter, enter)
	if m.screen != screenGate || len(m.gate) != 0 {
		t.Fatalf("screen %v, gate %v", m.screen, m.gate)
	}
	m, cmd := press(t, m, enter)
	m = drain(t, m, cmd)
	if !strings.Contains(strings.Join(ex.ran, ","), "compact:Ubuntu") {
		t.Fatalf("ran %v", ex.ran)
	}
}

func TestCtrlCDuringARunStopsBeforeTheNextStepInsteadOfQuitting(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m, _ = press(t, m, enter, enter)
	m.current = &plan.Step{ID: "compact:Ubuntu", Critical: true}

	m, cmd := press(t, m, ctrlC)
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("ctrl+c quit in the middle of a run")
		}
	}
	if !m.interrupted || m.ctx.Err() == nil {
		t.Fatal("the run was not told to stop")
	}
	if !strings.Contains(m.View(), "compaction") {
		t.Fatalf("the screen does not say it is waiting for the compaction:\n%s", m.View())
	}
}

func TestSummaryListsFailures(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m.screen = screenRun
	next, _ := m.Update(runDone{outcomes: []plan.Outcome{
		{Step: plan.Step{ID: "win:temp", Title: "%TEMP%"}, Status: plan.Done, Freed: 1 << 30},
		{Step: plan.Step{ID: "docker:buildcache", Title: "Build cache"}, Status: plan.Failed, Err: errors.New("daemon not answering")},
	}})
	m = next.(Model)
	s := m.Summary()
	for _, want := range []string{"daemon not answering", m.deps.LogPath} {
		if !strings.Contains(s, want) {
			t.Errorf("summary does not contain %q:\n%s", want, s)
		}
	}
}

func TestFilterTurnsAnInterruptIntoCtrlC(t *testing.T) {
	got := Filter(nil, tea.InterruptMsg{})
	key, ok := got.(tea.KeyMsg)
	if !ok || key.String() != "ctrl+c" {
		t.Fatalf("Filter(InterruptMsg{}) = %#v", got)
	}
	if got := Filter(nil, enter); !reflect.DeepEqual(got, tea.Msg(enter)) {
		t.Fatalf("Filter changed an unrelated message: %#v", got)
	}
}
