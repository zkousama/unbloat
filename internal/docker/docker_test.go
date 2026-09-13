package docker

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zkousama/unbloat/internal/run"
)

const exe = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseSizeUsesDockersDecimalUnits(t *testing.T) {
	for in, want := range map[string]int64{
		"8.357GB":        8_357_000_000,
		"4.628GB (55%)":  4_628_000_000,
		"0B":             0,
		"512kB":          512_000,
		"1.5MB":          1_500_000,
		"N/A":            0,
		"4.277GB (100%)": 4_277_000_000,
	} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseSize("12 parsecs"); err == nil {
		t.Error("want an error for an unknown unit")
	}
}

func TestParseSystemDFReadsTheRecordedOutput(t *testing.T) {
	df, err := ParseSystemDF(fixture(t, "system-df-before.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := df["Build Cache"]; got.Size != 12_640_000_000 || got.Count != 65 {
		t.Errorf("build cache = %+v", got)
	}
	if got := df["Images"]; got.Reclaimable != 4_628_000_000 {
		t.Errorf("images = %+v", got)
	}
	if got := df["Local Volumes"]; got.Count != 35 {
		t.Errorf("volumes = %+v", got)
	}
}

func TestParseVolumesTellsNamedFromAnonymous(t *testing.T) {
	vols, err := ParseVolumes(fixture(t, "volumes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	named, anonymous := 0, 0
	for _, v := range vols {
		if v.Anonymous {
			anonymous++
		} else {
			named++
			if !strings.HasPrefix(v.Name, "shop_") {
				t.Errorf("%s classed as named", v.Name)
			}
		}
	}
	if named != 2 || anonymous != 3 {
		t.Fatalf("named %d anonymous %d, want 2 and 3", named, anonymous)
	}
}

func TestRunningIsTheEngineAnswering(t *testing.T) {
	up := run.NewFake().On(run.Out("Docker Desktop\n"), exe, "info", "--format", "{{.OperatingSystem}}")
	if !(Client{R: up, Exe: exe}).Running(context.Background()) {
		t.Error("engine answered, want running")
	}
	down := run.NewFake().On(run.Exit(1, ""), exe, "info", "--format", "{{.OperatingSystem}}")
	if (Client{R: down, Exe: exe}).Running(context.Background()) {
		t.Error("engine did not answer, want not running")
	}
}

// A DOCKER_HOST or another context can point docker.exe at an engine that
// isn't Docker Desktop's. Nothing of that engine's is ever cleaned.
func TestAnotherEngineIsNotDockerDesktopRunning(t *testing.T) {
	other := run.NewFake().On(run.Out("Ubuntu 24.04.1 LTS\n"), exe, "info", "--format", "{{.OperatingSystem}}")
	if (Client{R: other, Exe: exe}).Running(context.Background()) {
		t.Error("an engine on Ubuntu answered, want not running")
	}
}

func TestClientCommands(t *testing.T) {
	f := run.NewFake().
		On(run.Out(fixture(t, "system-df.jsonl")), exe, "system", "df", "--format", "{{json .}}").
		On(run.Out(fixture(t, "volumes.jsonl")), exe, "volume", "ls", "--format", "{{json .}}").
		On(run.Out(""), exe, "builder", "prune", "-af").
		On(run.Out(""), exe, "image", "prune", "-af").
		On(run.Out(""), exe, "desktop", "stop")
	c := Client{R: f, Exe: exe}
	ctx := context.Background()

	if df, err := c.SystemDF(ctx); err != nil || df["Build Cache"].Size != 0 {
		t.Errorf("system df %+v, %v", df, err)
	}
	if vols, err := c.Volumes(ctx); err != nil || len(vols) != 5 {
		t.Errorf("volumes %d, %v", len(vols), err)
	}
	for name, fn := range map[string]func(context.Context) error{
		"builder prune": c.PruneBuildCache,
		"image prune":   c.PruneImages,
		"desktop stop":  c.Stop,
	} {
		if err := fn(ctx); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAFailedStopIsAnError(t *testing.T) {
	f := run.NewFake().On(run.Result{Code: 1, Stderr: []byte("could not stop")}, exe, "desktop", "stop")
	err := Client{R: f, Exe: exe}.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "could not stop") {
		t.Fatalf("err = %v", err)
	}
}

func TestLocatePrefersThePath(t *testing.T) {
	onPath := func(string) (string, error) { return `C:\bin\docker.exe`, nil }
	if got := Locate(onPath, func(string) bool { return true }, `C:\Program Files`); got != `C:\bin\docker.exe` {
		t.Fatalf("got %q", got)
	}
}

func TestLocateFallsBackToDockerDesktopsInstall(t *testing.T) {
	notOnPath := func(string) (string, error) { return "", errors.New("not found") }
	want := `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	exists := func(p string) bool { return p == want }
	if got := Locate(notOnPath, exists, `C:\Program Files`); got != want {
		t.Fatalf("got %q", got)
	}
	if got := Locate(notOnPath, func(string) bool { return false }, `C:\Program Files`); got != "" {
		t.Fatalf("want empty when docker is nowhere, got %q", got)
	}
}

// The registered docker-desktop distro is Docker's small system disk under
// ...\wsl\main. Its data disk is the sibling ...\wsl\disk\docker_data.vhdx, so
// a moved disk location is followed without reading Docker's settings.
func TestDataDiskIsTheSiblingOfTheRegisteredDisk(t *testing.T) {
	base := `D:\docker-data\wsl\main`
	want := `D:\docker-data\wsl\disk\docker_data.vhdx`
	got := DataDisk(base, `C:\Users\dev\AppData\Local`, func(p string) bool { return p == want })
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestDataDiskFallsBackToTheDefaultLocation(t *testing.T) {
	want := `C:\Users\dev\AppData\Local\Docker\wsl\disk\docker_data.vhdx`
	got := DataDisk("", `C:\Users\dev\AppData\Local`, func(p string) bool { return p == want })
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := DataDisk("", `C:\Users\dev\AppData\Local`, func(string) bool { return false }); got != "" {
		t.Fatalf("want empty when there is no disk, got %q", got)
	}
}
