package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zkousama/unbloat/internal/docker"
	"github.com/zkousama/unbloat/internal/execute"
	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/run"
	"github.com/zkousama/unbloat/internal/scan"
	"github.com/zkousama/unbloat/internal/sys"
	"github.com/zkousama/unbloat/internal/ui"
	"github.com/zkousama/unbloat/internal/vhd"
	"github.com/zkousama/unbloat/internal/wsl"
)

// version is set at release time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println(resolveVersion(version, debug.ReadBuildInfo))
		return
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "unbloat runs on Windows. It has to control WSL and Docker Desktop from outside them.")
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "--facts" {
		printFacts()
		return
	}
	os.Exit(start())
}

// printFacts shows what the safety checks see, without scanning anything.
func printFacts() {
	m := sys.Machine{}
	drive := systemDrive(os.Getenv)
	free, total, err := m.Space(drive)
	fmt.Printf("elevated   %v\n", m.Elevated())
	fmt.Printf("power      %+v\n", m.Power())
	fmt.Printf("ancestors  %s\n", strings.Join(m.Ancestors(), " < "))
	fmt.Printf("drive      %s %s free of %s (err %v)\n", drive, plan.Human(free), plan.Human(total), err)
}

func start() int {
	local := os.Getenv("LOCALAPPDATA")
	logPath, logFile, err := openLog(local, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "unbloat could not create its log file:", err)
		return 1
	}
	defer logFile.Close()

	// pnpm writes a temp file into its working directory, and a standard user
	// can't write to SystemRoot. LOCALAPPDATA is writable and survives WSL
	// shutting down.
	runner := &run.Exec{Log: logFile, Dir: local}
	facts := sys.Machine{}
	wslClient := wsl.Client{R: runner}
	exists := func(p string) bool { _, ok := stat(p); return ok }

	var dockerClient *docker.Client
	if exe := docker.Locate(exec.LookPath, exists, os.Getenv("ProgramFiles")); exe != "" {
		dockerClient = &docker.Client{R: runner, Exe: exe}
	}
	compactor := vhd.Compactor{R: runner, WriteScript: vhd.TempScript}

	ex := &execute.Executor{
		WSL:          wslClient,
		Docker:       dockerClient,
		Runner:       runner,
		Compactor:    compactor,
		Facts:        facts,
		LocalAppData: local,
		UserProfile:  os.Getenv("USERPROFILE"),
		SystemRoot:   os.Getenv("SystemRoot"),
		Stat:         stat,
		Now:          time.Now,
		UnlockTries:  15,
	}

	scanMachine := func(ctx context.Context, report scan.Report) plan.Plan {
		// Only the confirmation screen uses this, and it is shown after the
		// scan, so finding out here keeps it off the startup path.
		ex.Optimize = compactor.HasOptimizeVHD(ctx)
		return scan.Scan(ctx, scan.Sources{
			WSL:          wslClient,
			Docker:       dockerClient,
			Runner:       runner,
			Facts:        facts,
			LookPath:     exec.LookPath,
			Stat:         stat,
			LocalAppData: local,
			UserProfile:  os.Getenv("USERPROFILE"),
			SystemRoot:   os.Getenv("SystemRoot"),
			Temp:         os.TempDir(),
			Now:          time.Now(),
		}, report)
	}

	model := ui.New(ui.Deps{
		Facts:    facts,
		Scan:     scanMachine,
		Executor: ex,
		Describe: func(s plan.Step) []string { return ex.Describe(s) },
		Drive:    systemDrive(os.Getenv),
		LogPath:  logPath,
	})
	// Bubble Tea stops listening for interrupts after the first one. Without a
	// registration of our own, Go would fall back to its default handler for
	// the second and end the process, even partway through a compaction.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	final, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithFilter(ui.Filter)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "unbloat:", err)
		return 1
	}
	switch m := final.(ui.Model); {
	case m.Relaunched():
		fmt.Println("unbloat continues in the administrator window.")
	case m.Finished():
		fmt.Print(m.Summary())
	}
	return 0
}
