package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/sys"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// 2 totals, never one: space freed inside a disk that is not being compacted
// does not reach C:.
func TestChecklistShowsTwoTotals(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}, &recordingExecutor{})
	v := plain(m.View())
	for _, want := range []string{"freed inside WSL and Docker", "12.0 GB", "reaches C: only after compacting", "freed on C: by this run", "1.0 GB"} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist does not show %q:\n%s", want, v)
		}
	}

	m, _ = press(t, m, down, down, space) // compact Ubuntu, which Docker's cache is not inside
	v = plain(m.View())
	if !strings.Contains(v, "15.0 GB") {
		t.Errorf("ticking Ubuntu's compaction should put 15.0 GB on C:\n%s", v)
	}
}

func TestChecklistRows(t *testing.T) {
	m := checklist(t, &sys.FakeFacts{IsElevated: true, Free: 5 << 30, Total: 476 << 30}, &recordingExecutor{})
	v := plain(m.View())
	for _, want := range []string{
		"C: 5.0 GB free of 476.0 GB",
		"Volumes are kept", "shop_postgres-data",
		"[x] Build cache",
		"[ ] Compact Ubuntu", "up to 14.0 GB",
		"needs administrator rights",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("checklist does not show %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "[ ] Volumes") || strings.Contains(v, "[x] Volumes") {
		t.Error("the volumes note is drawn as something to tick")
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
