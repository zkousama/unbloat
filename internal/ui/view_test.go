package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/sys"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// 2 totals, never one: space freed inside a disk that is not being compacted
// does not reach C:. Ticking a compaction never moves its share onto C:
// either, since compacting returns an amount nobody can know in advance.
func TestChecklistShowsTwoTotals(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}, &recordingExecutor{})
	v := plain(m.View())
	for _, want := range []string{"freed inside WSL and Docker", "12.0 GB", "reaches C: only after compacting", "freed on C: by this run", "1.0 GB"} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist does not show %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "plus what compacting returns") {
		t.Errorf("the compacting line shows before anything is ticked:\n%s", v)
	}

	m, _ = press(t, m, down, down, space) // compact Ubuntu, which Docker's cache is not inside
	v = plain(m.View())
	if !strings.Contains(v, "12.0 GB") || !strings.Contains(v, "1.0 GB") {
		t.Errorf("ticking Ubuntu's compaction should leave the totals unchanged:\n%s", v)
	}
	if !strings.Contains(v, "plus what compacting returns, often less than a disk's file size minus its use") {
		t.Errorf("the compacting line is missing once Ubuntu is ticked:\n%s", v)
	}
}

func TestChecklistRows(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}, &recordingExecutor{})
	v := plain(m.View())
	for _, want := range []string{
		"C: 5.0 GB free of 476.0 GB",
		"Volumes are kept", "shop_postgres-data",
		"[x] Build cache",
		"[ ] Compact Ubuntu", "37.0 GB file",
		"needs administrator rights",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist does not show %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "up to 14.0 GB") {
		t.Errorf("the compaction row still shows an amount it may not return:\n%s", v)
	}
	if strings.Contains(v, "[ ] Volumes") || strings.Contains(v, "[x] Volumes") {
		t.Error("the volumes note is drawn as something to tick")
	}
}

// pnpm's size is the whole store: a prune only removes packages nothing
// references, so the row must not read as an amount the prune returns.
func TestChecklistLabelsPnpmRowsAsStores(t *testing.T) {
	gib := int64(1) << 30
	size := int64(3.7 * float64(gib))
	m := Model{screen: screenChecklist, plan: plan.Plan{Items: []plan.Item{
		{ID: "cache:Ubuntu:pnpm", Section: "WSL", Kind: plan.KindWSLPnpmPrune, Title: "pnpm store", Size: size, Selected: true},
		{ID: "win:pnpm", Section: "Windows", Kind: plan.KindWindowsTool, Tool: "pnpm", Title: "pnpm", Size: size, Selected: true},
		{ID: "win:npm", Section: "Windows", Kind: plan.KindWindowsTool, Tool: "npm", Title: "npm", Size: size, Selected: true},
	}}}
	v := plain(m.View())
	if strings.Count(v, "3.7 GB store") != 2 {
		t.Errorf("want the WSL and Windows pnpm rows labelled as a store:\n%s", v)
	}
	if !strings.Contains(v, "3.7 GB\n") {
		t.Errorf("npm's own row should show a plain size, not a store label:\n%s", v)
	}
}

func TestConfirmationListsStepsInRunOrderWithCommandsOnRequest(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m, _ = press(t, m, enter)
	v := plain(m.View())
	if strings.Index(v, "%TEMP%") > strings.Index(v, "Build cache") {
		t.Errorf("steps out of order:\n%s", v)
	}
	if strings.Contains(v, "run win:temp") {
		t.Error("commands shown before asking")
	}
	m, _ = press(t, m, letter('c'))
	if !strings.Contains(plain(m.View()), "run win:temp") {
		t.Errorf("commands not shown after c:\n%s", plain(m.View()))
	}
}

func TestGateNamesWhatWillStop(t *testing.T) {
	facts := &sys.FakeFacts{IsElevated: true, Chain: []string{"wsl.exe"}}
	m := checklist(t, facts, &recordingExecutor{})
	m, _ = press(t, m, down, down, space, enter, enter)
	v := plain(m.View())
	for _, want := range []string{"Ubuntu", "Docker Desktop", "inside WSL", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("gate does not show %q:\n%s", want, v)
		}
	}
}

func TestSummarySaysWSLAndDockerAreStopped(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m.screen = screenRun
	next, _ := m.Update(runDone{outcomes: []plan.Outcome{
		{Step: plan.Step{ID: "stop", Phase: plan.PhaseStop}, Status: plan.Done},
	}})
	if s := next.(Model).Summary(); !strings.Contains(s, "stopped") || !strings.Contains(s, "Docker Desktop") {
		t.Fatalf("summary:\n%s", s)
	}
}

// The estimate on the checklist is never a promise, so the summary reports
// what each compaction actually returned.
func TestSummaryReportsWhatEachCompactionReturned(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m.screen = screenRun
	next, _ := m.Update(runDone{outcomes: []plan.Outcome{
		{Step: plan.Step{ID: "compact:Ubuntu", Title: "Compact Ubuntu", Phase: plan.PhaseCompact}, Status: plan.Done, Freed: 4939212390},
		{Step: plan.Step{ID: "compact:docker", Title: "Compact Docker", Phase: plan.PhaseCompact}, Status: plan.Done, Freed: 0},
	}})
	s := next.(Model).Summary()
	if !strings.Contains(s, "Compacted:\n") {
		t.Fatalf("summary does not have a Compacted section:\n%s", s)
	}
	for _, want := range []string{"Compact Ubuntu: returned 4.6 GB", "Compact Docker: returned 0 B"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary does not show %q:\n%s", want, s)
		}
	}
}

func TestSummaryLeavesDockerOutWhenItWasNotRunning(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	m.plan.DockerRunning = false
	m.screen = screenRun
	next, _ := m.Update(runDone{outcomes: []plan.Outcome{
		{Step: plan.Step{ID: "stop", Phase: plan.PhaseStop}, Status: plan.Done},
	}})
	s := next.(Model).Summary()
	if !strings.Contains(s, "WSL is stopped. Open a WSL window to start it again.") || strings.Contains(s, "Docker Desktop") {
		t.Fatalf("summary:\n%s", s)
	}
}

func TestSummaryHeaderShowsTheFreeSpaceAfterTheRun(t *testing.T) {
	facts := &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}
	m := checklist(t, facts, &recordingExecutor{})
	facts.Free = 20 << 30
	m.screen = screenRun
	next, _ := m.Update(runDone{})
	v := plain(next.(Model).View())
	if !strings.Contains(v, "C: 20.0 GB free of 476.0 GB") || !strings.Contains(v, "C: had 5.0 GB free and now has 20.0 GB free.") {
		t.Fatalf("summary screen:\n%s", v)
	}
}

// failingSpace answers the first Space call and fails every one after it.
type failingSpace struct {
	*sys.FakeFacts
	calls int
}

func (f *failingSpace) Space(path string) (int64, int64, error) {
	if f.calls++; f.calls > 1 {
		return 0, 0, errors.New("the device is not ready")
	}
	return f.FakeFacts.Space(path)
}

// A free space that could not be read is not 0 B free.
func TestSummaryLeavesOutBeforeAndAfterWhenTheSecondReadingFails(t *testing.T) {
	d := deps(nil, &recordingExecutor{})
	d.Facts = &failingSpace{FakeFacts: &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}}
	m := New(d)
	m = drain(t, m, m.Init())
	m.screen = screenRun
	next, _ := m.Update(runDone{})
	m = next.(Model)
	if s := m.Summary(); strings.Contains(s, "free") {
		t.Fatalf("summary:\n%s", s)
	}
	if v := plain(m.View()); strings.Contains(v, "0 B") {
		t.Fatalf("summary screen:\n%s", v)
	}
}

func TestWindowKeepsTheCursorOnScreen(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", i)
	}
	got := window(lines, 80, 20)
	if len(got) != 20 {
		t.Fatalf("%d lines", len(got))
	}
	found := false
	for _, l := range got {
		if l == lines[80] {
			found = true
		}
	}
	if !found {
		t.Fatal("the focused line is not in the window")
	}
	if got := window(lines[:5], 3, 20); len(got) != 5 {
		t.Fatalf("a short list was cut to %d lines", len(got))
	}
}

// The confirmation screen has no cursor of its own, so Bubble Tea's own
// windowing (keeping only the bottom `height` lines) would scroll the first
// steps off the top as soon as commands are shown. It must scroll instead.
func TestConfirmationScrolls(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true}, &recordingExecutor{})
	next, _ := m.Update(tea.WindowSizeMsg{Height: 12, Width: 80})
	m = next.(Model)
	m, _ = press(t, m, down, down, space) // also compact Ubuntu, for enough steps to overflow
	m, _ = press(t, m, letter('c'))

	v := plain(m.View())
	if !strings.Contains(v, "%TEMP%") {
		t.Fatalf("the first step is not visible at the top:\n%s", v)
	}

	for i := 0; i < 20; i++ {
		m, _ = press(t, m, down)
	}
	v = plain(m.View())
	if strings.Contains(v, "%TEMP%") {
		t.Errorf("the first step is still visible after scrolling down:\n%s", v)
	}
	if !strings.Contains(v, "Compact Ubuntu") {
		t.Errorf("a later step is not visible after scrolling down:\n%s", v)
	}
}
