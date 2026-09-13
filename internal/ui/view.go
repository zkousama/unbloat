package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/zkousama/unbloat/internal/plan"
)

var (
	bold = lipgloss.NewStyle().Bold(true)
	dim  = lipgloss.NewStyle().Faint(true)
	warn = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	bad  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	good = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

func (m Model) View() string {
	var body string
	switch m.screen {
	case screenElevate:
		body = m.viewElevate()
	case screenScan:
		body = m.viewScan()
	case screenChecklist:
		body = m.viewChecklist()
	case screenConfirm:
		body = m.viewConfirm()
	case screenGate:
		body = m.viewGate()
	case screenRun:
		body = m.viewRun()
	case screenSummary:
		body = m.Summary() + "\n" + dim.Render("enter  quit")
	}
	out := m.header() + "\n\n" + body
	if m.message != "" {
		out += "\n\n" + warn.Render(m.message)
	}
	return out + "\n"
}

func (m Model) drive() string { return strings.TrimRight(m.deps.Drive, `\`) }

// header shows the free space now: before the run, and after it on the
// summary screen.
func (m Model) header() string {
	free := m.freeBefore
	if m.screen == screenSummary {
		free = m.freeAfter
	}
	if m.total <= 0 || (m.screen == screenSummary && m.afterUnknown) {
		return bold.Render("unbloat")
	}
	return bold.Render("unbloat") + fmt.Sprintf("   %s %s free of %s", m.drive(), plan.Human(free), plan.Human(m.total))
}

func (m Model) viewElevate() string {
	return "unbloat is not running as administrator. Compacting a disk needs it; everything else works without.\n\n" +
		"y  start again as administrator\n" +
		"n  continue without compacting"
}

func (m Model) viewScan() string {
	var b strings.Builder
	b.WriteString("Scanning. Nothing is changed.\n\n")
	for _, t := range m.targets {
		if t.err != nil {
			fmt.Fprintf(&b, "  %s  %s\n", t.name, bad.Render("failed: "+t.err.Error()))
		} else {
			fmt.Fprintf(&b, "  %s  %s\n", t.name, good.Render("done"))
		}
	}
	return b.String()
}

func (m Model) totals() string {
	t := m.plan.Totals()
	return fmt.Sprintf("freed inside WSL and Docker  %9s   reaches C: only after compacting\n", plan.Human(t.InsideDisks)) +
		fmt.Sprintf("freed on C: by this run      %9s", plan.Human(t.OnC))
}

func (m Model) viewChecklist() string {
	rows := m.rows()
	var lines []string
	focus, section := 0, ""
	for i, it := range m.plan.Items {
		if it.Section != section {
			if section != "" {
				lines = append(lines, "")
			}
			section = it.Section
			lines = append(lines, bold.Render(section))
		}

		if it.Kind == plan.KindNote {
			line := "      " + it.Title
			if it.Size > 0 {
				line += "  " + plan.Human(it.Size)
			}
			lines = append(lines, line)
			if it.Detail != "" {
				lines = append(lines, "      "+dim.Render(it.Detail))
			}
			continue
		}

		pointer := "  "
		if len(rows) > 0 && rows[m.cursor] == i {
			pointer, focus = "> ", len(lines)
		}
		box := "[ ]"
		if it.Selected {
			box = "[x]"
		}
		title, size := it.Title, plan.Human(it.Size)
		detail := it.Cost
		if it.Kind == plan.KindCompact {
			title = "Compact " + it.Title
			size = "up to " + plan.Human(it.Frees)
			if it.Frees == 0 {
				size = "unknown"
			}
			detail = it.Detail + "; " + it.Cost
		}
		if it.Disabled != "" {
			detail = it.Disabled
		}
		line := fmt.Sprintf("%s%s %-40s %14s", pointer, box, title, size)
		if it.Disabled != "" {
			line = dim.Render(line)
		}
		lines = append(lines, line)
		if detail != "" {
			lines = append(lines, "      "+dim.Render(detail))
		}
	}

	if m.height > 0 {
		lines = window(lines, focus, max(m.height-10, 5))
	}
	return strings.Join(lines, "\n") + "\n\n" + m.totals() + "\n\n" +
		dim.Render("space  tick   enter  review   c  review with commands   q  quit")
}

// confirmLines is the numbered steps and, when requested, the commands each
// one runs. It is the part of the confirmation screen that can outgrow the
// terminal and so is the part that scrolls.
func (m Model) confirmLines() []string {
	var lines []string
	for i, s := range m.steps {
		title := s.Title
		if s.Phase < plan.PhaseTrim && s.Item.Section != "" {
			title += dim.Render("  " + s.Item.Section)
		}
		lines = append(lines, fmt.Sprintf("%2d. %s", i+1, title))
		if m.showCommands {
			for _, c := range m.deps.Describe(s) {
				lines = append(lines, "      "+dim.Render(c))
			}
		}
	}
	return lines
}

func (m Model) viewConfirm() string {
	lines := m.confirmLines()
	if h := max(m.height-10, 5); m.height > 0 && len(lines) > h {
		start := min(m.scroll, len(lines)-h)
		lines = lines[start : start+h]
	}

	var b strings.Builder
	b.WriteString("This runs, in this order:\n\n")
	b.WriteString(strings.Join(lines, "\n"))
	if m.plan.Compacting() {
		b.WriteString("\n\n" + warn.Render("Compacting stops Docker Desktop and shuts down WSL, which closes every WSL window."))
	}
	b.WriteString("\n\n" + m.totals() + "\n\n")
	b.WriteString(dim.Render("enter  continue   c  show commands   esc  back   up/down  scroll"))
	return b.String()
}

func (m Model) viewGate() string {
	var b strings.Builder
	if len(m.plan.Running) > 0 {
		b.WriteString("WSL shuts down, closing these distros and every window open in them: " + strings.Join(m.plan.Running, ", ") + ".\n")
	} else {
		b.WriteString("No WSL distro is running.\n")
	}
	if m.plan.DockerRunning {
		b.WriteString("Docker Desktop stops, and its containers with it.\n")
	} else {
		b.WriteString("Docker Desktop is not running.\n")
	}

	if len(m.gate) > 0 {
		b.WriteString("\n" + bad.Render("unbloat will not shut anything down:") + "\n")
		for _, r := range m.gate {
			b.WriteString("  " + r + "\n")
		}
		b.WriteString("\n" + dim.Render("esc  back, to untick the disks and run the rest   q  quit"))
		return b.String()
	}
	b.WriteString("\nSave your work in any WSL window first.\n\n")
	b.WriteString(dim.Render("enter  stop, shut down and compact   esc  back"))
	return b.String()
}

func outcomeText(o plan.Outcome) string {
	switch o.Status {
	case plan.Done:
		if o.Freed > 0 {
			return good.Render("done, " + plan.Human(o.Freed))
		}
		return good.Render("done")
	case plan.Failed:
		return bad.Render("failed: " + o.Err.Error())
	case plan.Skipped:
		return warn.Render(o.Reason)
	}
	return ""
}

func (m Model) viewRun() string {
	finished := map[string]plan.Outcome{}
	for _, o := range m.outcomes {
		finished[o.Step.ID] = o
	}
	var b strings.Builder
	for i, s := range m.steps {
		status := dim.Render("waiting")
		if o, ok := finished[s.ID]; ok {
			status = outcomeText(o)
		} else if m.current != nil && m.current.ID == s.ID {
			status = warn.Render("running")
		}
		fmt.Fprintf(&b, "%2d. %-45s %s\n", i+1, s.Title, status)
	}
	if m.interrupted {
		if m.current != nil && m.current.Critical {
			b.WriteString("\n" + warn.Render("Waiting for the compaction to finish. Stopping it partway can corrupt the disk."))
		} else {
			b.WriteString("\n" + warn.Render("Stopping once the current step finishes."))
		}
	}
	return b.String()
}

// Summary is plain text, so it can be printed again once the interface has
// closed.
func (m Model) Summary() string {
	var b strings.Builder
	if m.total > 0 && !m.afterUnknown {
		fmt.Fprintf(&b, "%s had %s free and now has %s free.\n", m.drive(), plan.Human(m.freeBefore), plan.Human(m.freeAfter))
	}

	var failed, skipped []plan.Outcome
	done, stopped := 0, false
	for _, o := range m.outcomes {
		switch o.Status {
		case plan.Done:
			done++
			stopped = stopped || o.Step.Phase == plan.PhaseStop
		case plan.Failed:
			failed = append(failed, o)
		case plan.Skipped:
			skipped = append(skipped, o)
		}
	}
	fmt.Fprintf(&b, "Steps: %d done, %d failed, %d skipped.\n", done, len(failed), len(skipped))
	if len(failed) > 0 {
		b.WriteString("\nFailed:\n")
		for _, o := range failed {
			fmt.Fprintf(&b, "  %s: %v\n", o.Step.Title, o.Err)
		}
	}
	if len(skipped) > 0 {
		b.WriteString("\nSkipped:\n")
		for _, o := range skipped {
			fmt.Fprintf(&b, "  %s: %s\n", o.Step.Title, o.Reason)
		}
	}
	if stopped {
		b.WriteString("\nWSL is stopped. Open a WSL window to start it again.\n")
		if m.plan.DockerRunning {
			b.WriteString("Docker Desktop is stopped too. Start it from the Start menu.\n")
		}
	}
	fmt.Fprintf(&b, "\nEvery command and its output is in %s\n", m.deps.LogPath)
	return b.String()
}

// window returns at most height lines, keeping lines[focus] in view.
func window(lines []string, focus, height int) []string {
	if len(lines) <= height {
		return lines
	}
	start := max(0, min(focus-height/2, len(lines)-height))
	return lines[start : start+height]
}
