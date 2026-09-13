package vhd

import (
	"context"
	"strings"
	"testing"

	"github.com/zkousama/unbloat/internal/run"
)

const disk = `C:\Users\dev\AppData\Local\Packages\CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc\LocalState\ext4.vhdx`

func ps(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

func TestPSQuote(t *testing.T) {
	if got := PSQuote(`C:\Users\O'Brien\ext4.vhdx`); got != `'C:\Users\O''Brien\ext4.vhdx'` {
		t.Fatalf("got %s", got)
	}
}

// Exactly the script run by hand when this was designed, which compacted both
// disks. readonly is what makes attaching a disk safe.
func TestDiskpartScript(t *testing.T) {
	want := "select vdisk file=\"" + disk + "\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\nexit\r\n"
	if got := DiskpartScript(disk); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestCompactUsesOptimizeVHDWhenTheEditionHasIt(t *testing.T) {
	f := run.NewFake().
		On(run.Out(""), "powershell.exe", ps(HasOptimizeScript)...).
		On(run.Out(""), "powershell.exe", ps(OptimizeScript(disk))...)
	c := Compactor{R: f, WriteScript: func(string) (string, func(), error) {
		t.Fatal("diskpart must not be used when Optimize-VHD exists")
		return "", nil, nil
	}}
	if err := c.Compact(context.Background(), disk); err != nil {
		t.Fatal(err)
	}
}

// Home edition has no Optimize-VHD.
func TestCompactFallsBackToDiskpart(t *testing.T) {
	cleaned := false
	f := run.NewFake().
		On(run.Exit(1, ""), "powershell.exe", ps(HasOptimizeScript)...).
		On(run.Out(""), "diskpart.exe", "/s", `C:\Temp\unbloat-1.txt`)
	var written string
	c := Compactor{R: f, WriteScript: func(content string) (string, func(), error) {
		written = content
		return `C:\Temp\unbloat-1.txt`, func() { cleaned = true }, nil
	}}
	if err := c.Compact(context.Background(), disk); err != nil {
		t.Fatal(err)
	}
	if written != DiskpartScript(disk) {
		t.Fatalf("wrote %q", written)
	}
	if !cleaned {
		t.Fatal("the script file was not removed")
	}
}

// diskpart's messages are localized, so success is judged by its exit code
// alone.
func TestCompactReportsAFailingDiskpart(t *testing.T) {
	cleaned := false
	f := run.NewFake().
		On(run.Exit(1, ""), "powershell.exe", ps(HasOptimizeScript)...).
		On(run.Exit(2, "The process cannot access the file"), "diskpart.exe", "/s", `C:\Temp\s.txt`)
	c := Compactor{R: f, WriteScript: func(string) (string, func(), error) {
		return `C:\Temp\s.txt`, func() { cleaned = true }, nil
	}}
	err := c.Compact(context.Background(), disk)
	if err == nil || !strings.Contains(err.Error(), "cannot access") {
		t.Fatalf("err = %v", err)
	}
	if !cleaned {
		t.Fatal("the script file was not removed after a failure")
	}
}

func TestCompactReportsAFailingOptimizeVHD(t *testing.T) {
	f := run.NewFake().
		On(run.Out(""), "powershell.exe", ps(HasOptimizeScript)...).
		On(run.Result{Code: 1, Stderr: []byte("in use")}, "powershell.exe", ps(OptimizeScript(disk))...)
	err := Compactor{R: f}.Compact(context.Background(), disk)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("err = %v", err)
	}
}

func TestCommandsShowWhatWillRun(t *testing.T) {
	if got := Commands(disk, true); len(got) != 1 || !strings.Contains(got[0], "Optimize-VHD") {
		t.Errorf("optimize: %v", got)
	}
	got := Commands(disk, false)
	if len(got) != 6 || got[0] != "diskpart /s:" || !strings.Contains(got[2], "attach vdisk readonly") {
		t.Errorf("diskpart: %q", got)
	}
}

func TestCompactRefusesAPathDiskpartCannotQuote(t *testing.T) {
	f := run.NewFake()
	c := Compactor{R: f, WriteScript: func(string) (string, func(), error) {
		t.Fatal("WriteScript must not be called")
		return "", nil, nil
	}}
	err := c.Compact(context.Background(), `C:\bad"name.vhdx`)
	if err == nil || !strings.Contains(err.Error(), "cannot quote") {
		t.Fatalf("err = %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("no commands should have run, but got %v", f.Calls())
	}
}
