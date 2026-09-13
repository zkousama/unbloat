package wsl

import (
	"context"
	"fmt"
	"strings"

	"github.com/zkousama/unbloat/internal/run"
)

const exe = "wsl.exe"

// Distro is a registered distro with its running state and its disk.
type Distro struct {
	Name     string
	Running  bool
	Version  int
	BasePath string
	VhdFile  string
}

// Client runs wsl.exe and reads WSL's registry key. Commands inside a distro
// always go through -e, which runs the program directly instead of through
// the user's shell and its startup files.
type Client struct {
	R run.Runner
}

func (c Client) check(ctx context.Context, name string, args ...string) (run.Result, error) {
	res, err := c.R.Run(ctx, name, args...)
	if err != nil {
		return res, fmt.Errorf("%s: %w", run.Key(name, args...), err)
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(Decode(res.Stderr) + " " + Decode(res.Stdout))
		return res, fmt.Errorf("%s: exit %d: %s", run.Key(name, args...), res.Code, msg)
	}
	return res, nil
}

// Registrations reads every distro's registry entry.
func (c Client) Registrations(ctx context.Context) ([]Registration, error) {
	res, err := c.check(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", RegistryScript)
	if err != nil {
		return nil, err
	}
	return ParseRegistrations(Decode(res.Stdout))
}

// Distros lists every distro with its running state and disk.
func (c Client) Distros(ctx context.Context) ([]Distro, error) {
	all, err := c.check(ctx, exe, "--list", "--quiet")
	if err != nil {
		return nil, err
	}
	running := map[string]bool{}
	// When there are no running distros wsl.exe may exit non-zero with a
	// localized message. Treating that as "nothing running" is safe: a stopped
	// distro is never measured, so the scan never starts one.
	if res, err := c.check(ctx, exe, "--list", "--running", "--quiet"); err == nil {
		for _, name := range ParseNames(Decode(res.Stdout)) {
			running[name] = true
		}
	}
	regs, err := c.Registrations(ctx)
	if err != nil {
		return nil, err
	}
	byName := map[string]Registration{}
	for _, r := range regs {
		byName[r.Name] = r
	}

	var distros []Distro
	for _, name := range ParseNames(Decode(all.Stdout)) {
		r := byName[name]
		distros = append(distros, Distro{
			Name:     name,
			Running:  running[name],
			Version:  r.Version,
			BasePath: r.BasePath,
			VhdFile:  r.VhdFile,
		})
	}
	return distros, nil
}

// Used returns the bytes in use on the filesystem holding path.
func (c Client) Used(ctx context.Context, distro, path string) (int64, error) {
	res, err := c.check(ctx, exe, "-d", distro, "-u", "root", "-e", "df", "-Pk", path)
	if err != nil {
		return 0, err
	}
	return ParseDfUsed(Decode(res.Stdout))
}

// Trim tells the filesystem which blocks are free, so a compaction can find
// them. It runs as root, so it never waits on a sudo password. An empty mount
// trims every mounted filesystem.
func (c Client) Trim(ctx context.Context, distro, mount string) error {
	_, err := c.check(ctx, exe, "-d", distro, "-u", "root", "-e", "sh", "-c", TrimScript(mount))
	return err
}

// TrimScript is the script Trim runs inside the distro. wsl.exe's -e looks
// programs up without /usr/sbin and /sbin, where fstrim lives, so the script
// adds them to PATH itself before running it. Running it through a
// non-interactive sh still avoids the user's shell configuration: such a
// shell reads no startup files.
func TrimScript(mount string) string {
	if mount == "" {
		return `PATH="$PATH:/usr/sbin:/sbin"; export PATH; exec fstrim -av`
	}
	return `PATH="$PATH:/usr/sbin:/sbin"; export PATH; exec fstrim -v ` + shellQuote(mount)
}

// shellQuote quotes s for sh. Duplicated from internal/caches, which this
// package does not import.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Shutdown stops every distro and the WSL virtual machine.
func (c Client) Shutdown(ctx context.Context) error {
	_, err := c.check(ctx, exe, "--shutdown")
	return err
}

// Script runs a shell script as the distro's default user. The caller reads
// the exit code; an error means wsl.exe itself could not run.
func (c Client) Script(ctx context.Context, distro, script string) (run.Result, error) {
	return c.R.Run(ctx, exe, "-d", distro, "-e", "sh", "-c", script)
}
