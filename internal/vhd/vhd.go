// Package vhd compacts virtual disk files, returning the space freed inside
// them to the drive they sit on.
package vhd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/zkousama/unbloat/internal/run"
)

// HasOptimizeScript exits 0 when this Windows has Optimize-VHD, which comes
// with Hyper-V's PowerShell module: Pro, Enterprise and Education, not Home.
const HasOptimizeScript = "if (Get-Command Optimize-VHD -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"

// PSQuote quotes s as a PowerShell single-quoted string.
func PSQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// OptimizeScript compacts a disk with Optimize-VHD. Full mode reclaims more
// than diskpart can, because diskpart only returns whole unused blocks.
func OptimizeScript(path string) string {
	return "Optimize-VHD -Path " + PSQuote(path) + " -Mode Full"
}

// DiskpartScript compacts a disk on any edition. Attaching it read-only is
// what makes attaching it safe.
func DiskpartScript(path string) string {
	return strings.Join([]string{
		`select vdisk file="` + path + `"`,
		"attach vdisk readonly",
		"compact vdisk",
		"detach vdisk",
		"exit",
		"",
	}, "\r\n")
}

// Compactor compacts a disk. The disk must not be in use, which the caller
// checks first.
type Compactor struct {
	R           run.Runner
	WriteScript func(content string) (path string, cleanup func(), err error)
}

// TempScript writes content to a temporary file for diskpart /s.
func TempScript(content string) (string, func(), error) {
	f, err := os.CreateTemp("", "unbloat-*.txt")
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func powershell(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

func (c Compactor) HasOptimizeVHD(ctx context.Context) bool {
	res, err := c.R.Run(ctx, "powershell.exe", powershell(HasOptimizeScript)...)
	return err == nil && res.Code == 0
}

func (c Compactor) Compact(ctx context.Context, path string) error {
	if c.HasOptimizeVHD(ctx) {
		res, err := c.R.Run(ctx, "powershell.exe", powershell(OptimizeScript(path))...)
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return fmt.Errorf("Optimize-VHD: exit %d: %s", res.Code, strings.TrimSpace(string(res.Stderr)))
		}
		return nil
	}

	script, cleanup, err := c.WriteScript(DiskpartScript(path))
	if err != nil {
		return fmt.Errorf("writing the diskpart script: %w", err)
	}
	defer cleanup()
	res, err := c.R.Run(ctx, "diskpart.exe", "/s", script)
	if err != nil {
		return err
	}
	// diskpart's messages are localized; its exit code is not.
	if res.Code != 0 {
		return fmt.Errorf("diskpart: exit %d: %s", res.Code, strings.TrimSpace(string(res.Stdout)))
	}
	return nil
}

// Commands is what Compact runs, written out for the confirmation screen.
func Commands(path string, optimize bool) []string {
	if optimize {
		return []string{"powershell " + OptimizeScript(path)}
	}
	lines := []string{"diskpart /s:"}
	for _, l := range strings.Split(strings.TrimRight(DiskpartScript(path), "\r\n"), "\r\n") {
		lines = append(lines, "  "+l)
	}
	return lines
}
