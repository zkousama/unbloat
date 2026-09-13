package wsl

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zkousama/unbloat/internal/run"
)

const dfUbuntu = "Filesystem     1024-blocks     Used Available Capacity Mounted on\n" +
	"/dev/sdd        1055762868 21896672 980162724       3% /\n"

// Docker's distro is busybox, which lays df out differently but still honours -P.
const dfDocker = "Filesystem           1024-blocks    Used Available Capacity Mounted on\n" +
	"/dev/sde             1055762868  14160136 987899260   1% /mnt/docker-desktop-disk\n"

func recorded(t *testing.T, name string) run.Result {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return run.Result{Stdout: b}
}

func TestParseRegistrationsReadsTheRecordedRegistry(t *testing.T) {
	b, _ := os.ReadFile("testdata/lxss.json")
	regs, err := ParseRegistrations(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 2 {
		t.Fatalf("got %d registrations", len(regs))
	}
	ubuntu := regs[0]
	if ubuntu.Name != "Ubuntu" || ubuntu.Version != 2 {
		t.Fatalf("got %+v", ubuntu)
	}
	want := `C:\Users\dev\AppData\Local\Packages\CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc\LocalState\ext4.vhdx`
	if ubuntu.VhdFile != want {
		t.Fatalf("vhd = %q", ubuntu.VhdFile)
	}
}

// Docker's registration carries a \\?\ long-path prefix, which Optimize-VHD and
// diskpart do not accept.
func TestParseRegistrationsRemovesTheLongPathPrefix(t *testing.T) {
	b, _ := os.ReadFile("testdata/lxss.json")
	regs, _ := ParseRegistrations(string(b))
	if got := regs[1].BasePath; got != `C:\Users\dev\AppData\Local\Docker\wsl\main` {
		t.Fatalf("base = %q", got)
	}
}

// With one distro, PowerShell prints an object instead of an array.
func TestParseRegistrationsAcceptsASingleObject(t *testing.T) {
	regs, err := ParseRegistrations(`{"DistributionName":"Debian","BasePath":"C:\\d","VhdFileName":null,"Version":2}`)
	if err != nil || len(regs) != 1 {
		t.Fatalf("regs %v, err %v", regs, err)
	}
	if regs[0].VhdFile != `C:\d\ext4.vhdx` {
		t.Fatalf("a missing VhdFileName should default to ext4.vhdx, got %q", regs[0].VhdFile)
	}
}

func TestParseRegistrationsAcceptsNoOutput(t *testing.T) {
	regs, err := ParseRegistrations("  \n")
	if err != nil || len(regs) != 0 {
		t.Fatalf("regs %v, err %v", regs, err)
	}
}

func TestParseDfUsed(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want int64
	}{
		"gnu":     {dfUbuntu, 21896672 * 1024},
		"busybox": {dfDocker, 14160136 * 1024},
	} {
		got, err := ParseDfUsed(tc.text)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %d, %v; want %d", name, got, err, tc.want)
		}
	}
	if _, err := ParseDfUsed("Filesystem only\n"); err == nil {
		t.Error("want an error when there is no data line")
	}
}

func TestDistrosJoinsNamesRunningStateAndDisks(t *testing.T) {
	f := run.NewFake().
		On(recorded(t, "list-quiet.utf16"), "wsl.exe", "--list", "--quiet").
		On(recorded(t, "list-running.utf16"), "wsl.exe", "--list", "--running", "--quiet").
		On(recorded(t, "lxss.json"), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", RegistryScript)

	distros, err := Client{R: f}.Distros(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(distros) != 2 {
		t.Fatalf("got %+v", distros)
	}
	if !distros[0].Running || distros[0].Name != "Ubuntu" || !strings.HasSuffix(distros[0].VhdFile, `LocalState\ext4.vhdx`) {
		t.Errorf("Ubuntu = %+v", distros[0])
	}
	if distros[1].Running || distros[1].Name != "docker-desktop" {
		t.Errorf("docker-desktop = %+v", distros[1])
	}
}

// If the running list cannot be read, treating everything as stopped is the
// safe direction: a stopped distro is never measured, so nothing gets started.
func TestDistrosTreatsAnUnreadableRunningListAsNothingRunning(t *testing.T) {
	f := run.NewFake().
		On(recorded(t, "list-quiet.utf16"), "wsl.exe", "--list", "--quiet").
		On(run.Exit(1, ""), "wsl.exe", "--list", "--running", "--quiet").
		On(recorded(t, "lxss.json"), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", RegistryScript)

	distros, err := Client{R: f}.Distros(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range distros {
		if d.Running {
			t.Errorf("%s reported running", d.Name)
		}
	}
}

func TestUsedRunsDfAsRootWithoutAShell(t *testing.T) {
	f := run.NewFake().On(run.Out(dfUbuntu), "wsl.exe", "-d", "Ubuntu", "-u", "root", "-e", "df", "-Pk", "/")
	got, err := Client{R: f}.Used(context.Background(), "Ubuntu", "/")
	if err != nil || got != 21896672*1024 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestTrimAllMountsOrOne(t *testing.T) {
	f := run.NewFake().
		On(run.Out(""), "wsl.exe", "-d", "Ubuntu", "-u", "root", "-e", "fstrim", "-av").
		On(run.Out(""), "wsl.exe", "-d", "docker-desktop", "-u", "root", "-e", "fstrim", "-v", "/mnt/docker-desktop-disk")
	c := Client{R: f}
	if err := c.Trim(context.Background(), "Ubuntu", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.Trim(context.Background(), "docker-desktop", "/mnt/docker-desktop-disk"); err != nil {
		t.Fatal(err)
	}
}

// wsl.exe's own error text is UTF-16. It has to be readable in the message.
func TestFailuresCarryWslsDecodedMessage(t *testing.T) {
	msg := []byte{'n', 0, 'o', 0, ' ', 0, 's', 0, 'u', 0, 'c', 0, 'h', 0}
	f := run.NewFake().On(run.Result{Code: 1, Stdout: msg}, "wsl.exe", "--shutdown")
	err := Client{R: f}.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no such") {
		t.Fatalf("err = %v", err)
	}
}

func TestScriptRunsAsTheDefaultUserThroughSh(t *testing.T) {
	f := run.NewFake().On(run.Exit(3, ""), "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", "exit 3")
	res, err := Client{R: f}.Script(context.Background(), "Ubuntu", "exit 3")
	if err != nil || res.Code != 3 {
		t.Fatalf("res %+v, err %v", res, err)
	}
}

func TestScriptReturnsAnErrorOnlyWhenWslCannotRun(t *testing.T) {
	boom := errors.New("wsl.exe not found")
	f := run.NewFake().Fails(boom, "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", "true")
	if _, err := (Client{R: f}).Script(context.Background(), "Ubuntu", "true"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
