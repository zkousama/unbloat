package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zkousama/unbloat/internal/caches"
	"github.com/zkousama/unbloat/internal/docker"
	"github.com/zkousama/unbloat/internal/plan"
	"github.com/zkousama/unbloat/internal/run"
	"github.com/zkousama/unbloat/internal/sys"
	"github.com/zkousama/unbloat/internal/wsl"
)

const (
	dockerExe  = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	ubuntuDisk = `C:\Users\dev\AppData\Local\Packages\CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc\LocalState\ext4.vhdx`
	dockerDisk = `C:\Users\dev\AppData\Local\Docker\wsl\disk\docker_data.vhdx`

	ubuntuDiskSize = 40_050_581_094 // 37.3 GB
	dockerDiskSize = 27_058_293_964 // 25.2 GB
	ubuntuUsedKB   = 21896672
	dockerUsedKB   = 14160136
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func df(usedKB int64, mount string) string {
	return "Filesystem 1024-blocks Used Available Capacity Mounted on\n" +
		"/dev/sdd 1055762868 " + strconv.FormatInt(usedKB, 10) + " 980162724 3% " + mount + "\n"
}

// recordedMachine is the machine the design was recorded on: Ubuntu running,
// docker-desktop registered, Docker Desktop answering.
func recordedMachine(t *testing.T) *run.Fake {
	t.Helper()
	return run.NewFake().
		On(run.Out(readFile(t, "../wsl/testdata/list-quiet.utf16")), "wsl.exe", "--list", "--quiet").
		On(run.Out(readFile(t, "../wsl/testdata/list-running.utf16")), "wsl.exe", "--list", "--running", "--quiet").
		On(run.Out(readFile(t, "../wsl/testdata/lxss.json")), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", wsl.RegistryScript).
		On(run.Out("1992400\t.npm/_cacache\n1468004\t.npm/_npx\n"), "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", caches.MeasureScript(caches.WSLCaches)).
		On(run.Exit(5, ""), "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", caches.PnpmMeasureScript).
		On(run.Out(df(ubuntuUsedKB, "/")), "wsl.exe", "-d", "Ubuntu", "-u", "root", "-e", "df", "-Pk", "/").
		On(run.Out("Docker Desktop\n"), dockerExe, "info", "--format", "{{.OperatingSystem}}").
		On(run.Out(readFile(t, "../docker/testdata/system-df-before.jsonl")), dockerExe, "system", "df", "--format", "{{json .}}").
		On(run.Out(readFile(t, "../docker/testdata/volumes.jsonl")), dockerExe, "volume", "ls", "--format", "{{json .}}").
		On(run.Out(df(dockerUsedKB, docker.DataMount)), "wsl.exe", "-d", docker.DistroName, "-u", "root", "-e", "df", "-Pk", docker.DataMount)
}

// tempDir is t.TempDir() without the test name in the path: t.TempDir()
// embeds t.Name(), and a test named after "volume" would then leak that word
// into every path built under it, tripping the volume-mention check below.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "unbloat-scan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func sources(t *testing.T, f *run.Fake, facts *sys.FakeFacts) Sources {
	t.Helper()
	now := time.Now()
	temp := tempDir(t)
	old := filepath.Join(temp, "old.tmp")
	if err := os.WriteFile(old, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, now.AddDate(0, 0, -30), now.AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	local := tempDir(t)
	npm := filepath.Join(local, "npm-cache", "_cacache", "index")
	if err := os.MkdirAll(filepath.Dir(npm), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(npm, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	disks := map[string]int64{ubuntuDisk: ubuntuDiskSize, dockerDisk: dockerDiskSize}
	return Sources{
		WSL:          wsl.Client{R: f},
		Docker:       &docker.Client{R: f, Exe: dockerExe},
		Runner:       f,
		Facts:        facts,
		LookPath:     func(string) (string, error) { return "", errors.New("not found") },
		Stat:         func(p string) (int64, bool) { s, ok := disks[p]; return s, ok },
		LocalAppData: local,
		Temp:         temp,
		Now:          now,
	}
}

func find(p plan.Plan, id string) (plan.Item, bool) {
	for _, it := range p.Items {
		if it.ID == id {
			return it, true
		}
	}
	return plan.Item{}, false
}

func TestScanFindsEverythingOnTheRecordedMachine(t *testing.T) {
	var reported []string
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true}),
		func(target string, err error) {
			if err != nil {
				t.Errorf("%s failed: %v", target, err)
			}
			reported = append(reported, target)
		})

	for _, id := range []string{"win:temp", "win:npm", "cache:Ubuntu:npm", "cache:Ubuntu:npx", "docker:buildcache", "docker:images", "docker:volumes", "compact:Ubuntu", "compact:docker"} {
		if _, ok := find(p, id); !ok {
			t.Errorf("no item %s", id)
		}
	}
	if len(p.Running) != 1 || p.Running[0] != "Ubuntu" {
		t.Errorf("running = %v", p.Running)
	}
	if !p.DockerRunning {
		t.Error("Docker answered, so it is running")
	}
	if strings.Join(reported, ",") != "Caches on Windows,WSL,Ubuntu,Docker" {
		t.Errorf("reported %v", reported)
	}
}

// Docker Desktop registers a distro of its own. Its space is the data disk,
// handled under Docker, so it is never listed as a distro.
func TestDockerDesktopIsNotTreatedAsADistro(t *testing.T) {
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true}), nil)
	for _, it := range p.Items {
		if strings.Contains(it.ID, docker.DistroName) || strings.Contains(it.Section, docker.DistroName) {
			t.Errorf("item %s in %q", it.ID, it.Section)
		}
	}
}

func TestNoVolumeIsEverSelectable(t *testing.T) {
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true}), nil)
	vols, _ := find(p, "docker:volumes")
	if vols.Kind != plan.KindNote {
		t.Fatalf("volumes are kind %v", vols.Kind)
	}
	if !strings.Contains(vols.Detail, "shop_") || !strings.Contains(vols.Detail, "3 anonymous") {
		t.Errorf("detail = %q", vols.Detail)
	}
	for _, it := range p.Items {
		if strings.Contains(strings.ToLower(it.Title+" "+it.Detail+" "+it.Path), "volume") && it.Kind != plan.KindNote {
			t.Errorf("%s mentions volumes and can be selected", it.ID)
		}
	}
	if err := p.Toggle("docker:volumes"); err == nil {
		t.Fatal("the volumes note was selected")
	}
}

func TestDefaults(t *testing.T) {
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true}), nil)
	for id, want := range map[string]bool{
		"win:temp":          true,
		"win:npm":           true,
		"cache:Ubuntu:npm":  true,
		"docker:buildcache": true,
		"docker:images":     false,
		"compact:Ubuntu":    false,
		"compact:docker":    false,
	} {
		if it, _ := find(p, id); it.Selected != want {
			t.Errorf("%s selected %v, want %v", id, it.Selected, want)
		}
	}
}

// The estimate is the disk file minus what is in use inside it.
func TestCompactionEstimate(t *testing.T) {
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true}), nil)
	ubuntu, _ := find(p, "compact:Ubuntu")
	if want := int64(ubuntuDiskSize - ubuntuUsedKB*1024); ubuntu.Frees != want {
		t.Errorf("Ubuntu frees %d, want %d", ubuntu.Frees, want)
	}
	dock, _ := find(p, "compact:docker")
	if dock.Path != dockerDisk || dock.Distro != docker.DistroName || dock.Mount != docker.DataMount {
		t.Errorf("docker disk = %+v", dock)
	}
}

func TestCompactionNeedsAdministratorRights(t *testing.T) {
	p := Scan(context.Background(), sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: false}), nil)
	for _, id := range []string{"compact:Ubuntu", "compact:docker"} {
		if it, _ := find(p, id); !strings.Contains(it.Disabled, "administrator") {
			t.Errorf("%s disabled = %q", id, it.Disabled)
		}
	}
}

func TestAStoppedDistroIsNeverStarted(t *testing.T) {
	f := recordedMachine(t).On(run.Out(""), "wsl.exe", "--list", "--running", "--quiet")
	p := Scan(context.Background(), sources(t, f, &sys.FakeFacts{IsElevated: true}), nil)

	for _, call := range f.Calls() {
		if strings.HasPrefix(call, "wsl.exe -d Ubuntu") {
			t.Errorf("the scan ran %q in a stopped distro", call)
		}
	}
	if _, ok := find(p, "wsl:Ubuntu:stopped"); !ok {
		t.Error("no note saying Ubuntu is stopped")
	}
	ubuntu, ok := find(p, "compact:Ubuntu")
	if !ok || ubuntu.Frees != 0 {
		t.Errorf("a stopped distro's disk is offered with an unknown estimate, got %+v", ubuntu)
	}
}

func TestNothingFromDockerWhenItIsNotRunning(t *testing.T) {
	f := recordedMachine(t).On(run.Exit(1, ""), dockerExe, "info", "--format", "{{.OperatingSystem}}")
	p := Scan(context.Background(), sources(t, f, &sys.FakeFacts{IsElevated: true}), nil)

	for _, it := range p.Items {
		if it.Disk == "docker" || strings.HasPrefix(it.ID, "compact:docker") {
			t.Errorf("offered %s with Docker stopped", it.ID)
		}
	}
	if _, ok := find(p, "docker:stopped"); !ok {
		t.Error("no note saying Docker Desktop is not running")
	}
	for _, call := range f.Calls() {
		if strings.Contains(call, "system df") || strings.Contains(call, docker.DataMount) {
			t.Errorf("ran %q with Docker stopped", call)
		}
	}
}

// %TEMP% can fall back to a folder that isn't a temp folder at all. It's
// refused before anything under it is read.
func TestATempFolderThatIsNotOneIsRefused(t *testing.T) {
	src := sources(t, recordedMachine(t), &sys.FakeFacts{IsElevated: true})
	src.Temp = `C:\Data`
	src.UserProfile, src.SystemRoot = `C:\Users\dev`, `C:\Windows`
	p := Scan(context.Background(), src, nil)

	note, ok := find(p, "win:temp:refused")
	if !ok || note.Kind != plan.KindNote || note.Section != "Caches on Windows" ||
		note.Title != "%TEMP% is not offered" || note.Detail != `It points at C:\Data, which doesn't look like a temp folder.` {
		t.Errorf("note = %+v", note)
	}
	if _, ok := find(p, "win:temp"); ok {
		t.Error("offered a folder that isn't a temp folder")
	}
}

func TestAnotherDockerEngineIsNeverCleaned(t *testing.T) {
	f := recordedMachine(t).On(run.Out("Ubuntu 24.04.1 LTS\n"), dockerExe, "info", "--format", "{{.OperatingSystem}}")
	p := Scan(context.Background(), sources(t, f, &sys.FakeFacts{IsElevated: true}), nil)

	for _, id := range []string{"docker:buildcache", "docker:images", "compact:docker"} {
		if _, ok := find(p, id); ok {
			t.Errorf("offered %s from an engine that isn't Docker Desktop's", id)
		}
	}
	if _, ok := find(p, "docker:stopped"); !ok {
		t.Error("no note saying Docker Desktop is not running")
	}
	for _, call := range f.Calls() {
		if strings.Contains(call, "system df") || strings.Contains(call, "docker-desktop-disk") {
			t.Errorf("ran %q against another engine", call)
		}
	}
}

func TestAFailedPartDoesNotStopTheScan(t *testing.T) {
	f := recordedMachine(t).On(run.Exit(1, ""), "wsl.exe", "-d", "Ubuntu", "-e", "sh", "-c", caches.MeasureScript(caches.WSLCaches))
	var failed []string
	p := Scan(context.Background(), sources(t, f, &sys.FakeFacts{IsElevated: true}), func(target string, err error) {
		if err != nil {
			failed = append(failed, target)
		}
	})
	if len(failed) != 1 {
		t.Fatalf("failed = %v", failed)
	}
	note, ok := find(p, "failed:"+failed[0])
	if !ok || note.Kind != plan.KindNote || note.Section != "Not scanned" {
		t.Errorf("failure note = %+v", note)
	}
	if _, ok := find(p, "docker:buildcache"); !ok {
		t.Error("Docker was not scanned after Ubuntu's caches failed")
	}
}
