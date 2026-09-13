// Package caches measures and clears package manager caches, inside WSL
// distros and on Windows.
package caches

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/zkousama/unbloat/internal/run"
)

// Scripter runs a shell script inside a distro as its default user.
type Scripter interface {
	Script(ctx context.Context, distro, script string) (run.Result, error)
}

// Cache is a directory its tool fully owns, so deleting it is the same as the
// tool's own clean command. Doing it directly means the tool does not have to
// be on the PATH of a non-interactive shell, which Node installed through nvm
// usually is not.
type Cache struct {
	ID    string
	Title string
	Dir   string // relative to the home directory
}

// WSLCaches are the caches measured and cleared inside each distro.
var WSLCaches = []Cache{
	{ID: "npm", Title: "npm cache", Dir: ".npm/_cacache"},
	{ID: "npx", Title: "npx cache", Dir: ".npm/_npx"},
	{ID: "pip", Title: "pip cache", Dir: ".cache/pip"},
}

// ShellQuote quotes s for sh.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// MeasureScript prints "<kilobytes>\t<dir>" for each cache that exists.
func MeasureScript(caches []Cache) string {
	var b strings.Builder
	b.WriteString(`cd "$HOME" || exit 1`)
	for _, c := range caches {
		q := ShellQuote(c.Dir)
		fmt.Fprintf(&b, "; [ -d %s ] && du -sk %s", q, q)
	}
	b.WriteString("; true")
	return b.String()
}

// ParseMeasure reads MeasureScript's output into cache ID to bytes.
func ParseMeasure(text string, caches []Cache) map[string]int64 {
	idByDir := map[string]string{}
	for _, c := range caches {
		idByDir[c.Dir] = c.ID
	}
	sizes := map[string]int64{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(fields) != 2 {
			continue
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		id, known := idByDir[strings.TrimSpace(fields[1])]
		if err == nil && known {
			sizes[id] = kb * 1024
		}
	}
	return sizes
}

// RemoveScript deletes one cache directory under the home directory.
func RemoveScript(dir string) string {
	return `cd "$HOME" || exit 1; rm -rf -- ` + ShellQuote(dir)
}

// pnpmFind sets P to a pnpm that runs inside the distro. A pnpm under /mnt is
// Windows' own, reached through interop, and would prune the wrong store.
const pnpmFind = `P=$(command -v pnpm 2>/dev/null); ` +
	`case "$P" in ""|/mnt/*) P="$HOME/.local/share/pnpm/pnpm" ;; esac; `

// PnpmMeasureScript prints "<kilobytes>\t<store>" and exits 0 when pnpm and
// its store are both there; exits 3, still printing the size, when the default
// store exists but pnpm cannot be run; 4 when pnpm runs but has no store; 5
// when there is neither. It starts from $HOME: pnpm picks its store by the
// filesystem it runs from, and a Windows working directory would put it under
// /mnt.
const PnpmMeasureScript = `cd "$HOME" || exit 1; ` + pnpmFind +
	`if [ -x "$P" ]; then S=$("$P" store path 2>/dev/null) || exit 4; [ -d "$S" ] || exit 4; du -sk "$S"; exit 0; fi; ` +
	`if [ -d "$HOME/.local/share/pnpm/store" ]; then du -sk "$HOME/.local/share/pnpm/store"; exit 3; fi; exit 5`

// PnpmPruneScript removes packages no project references. Only pnpm knows which
// those are, so there is no direct-deletion version. Exits 3 without pnpm.
const PnpmPruneScript = `cd "$HOME" || exit 1; ` + pnpmFind + `[ -x "$P" ] || exit 3; "$P" store prune`

// PnpmStore is what PnpmMeasureScript found.
type PnpmStore struct {
	Found  bool
	Exists bool
	Size   int64
}

// InDistro runs the cache scripts inside a distro.
type InDistro struct {
	S Scripter
}

func (d InDistro) Measure(ctx context.Context, distro string) (map[string]int64, error) {
	res, err := d.S.Script(ctx, distro, MeasureScript(WSLCaches))
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("measuring caches in %s: exit %d: %s", distro, res.Code, strings.TrimSpace(string(res.Stderr)))
	}
	return ParseMeasure(string(res.Stdout), WSLCaches), nil
}

// Remove deletes one of WSLCaches. Any other directory is refused before a
// shell runs: this is the only place a path turns into rm -rf.
func (d InDistro) Remove(ctx context.Context, distro, dir string) error {
	known := false
	for _, c := range WSLCaches {
		if c.Dir == dir {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("refusing to delete %q: it is not a cache unbloat knows", dir)
	}
	res, err := d.S.Script(ctx, distro, RemoveScript(dir))
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("deleting %s in %s: exit %d: %s", dir, distro, res.Code, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func (d InDistro) Pnpm(ctx context.Context, distro string) (PnpmStore, error) {
	res, err := d.S.Script(ctx, distro, PnpmMeasureScript)
	if err != nil {
		return PnpmStore{}, err
	}
	size := int64(0)
	if fields := strings.Fields(string(res.Stdout)); len(fields) > 0 {
		if kb, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			size = kb * 1024
		}
	}
	switch res.Code {
	case 0:
		return PnpmStore{Found: true, Exists: true, Size: size}, nil
	case 3:
		return PnpmStore{Exists: true, Size: size}, nil
	case 4:
		return PnpmStore{Found: true}, nil
	case 5:
		return PnpmStore{}, nil
	default:
		return PnpmStore{}, fmt.Errorf("measuring the pnpm store in %s: exit %d", distro, res.Code)
	}
}

func (d InDistro) PnpmPrune(ctx context.Context, distro string) error {
	res, err := d.S.Script(ctx, distro, PnpmPruneScript)
	if err != nil {
		return err
	}
	if res.Code == 3 {
		return fmt.Errorf("pnpm could not be found in %s from a non-interactive shell", distro)
	}
	if res.Code != 0 {
		return fmt.Errorf("pnpm store prune in %s: exit %d: %s", distro, res.Code, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}
