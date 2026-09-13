// Package docker reads and cleans Docker Desktop through docker.exe.
package docker

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var units = map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15}

// ParseSize reads a size as Docker prints it: "8.357GB", "4.628GB (55%)",
// "0B". Docker uses decimal units, so a GB here is 1e9 bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	if s == "" || s == "N/A" {
		return 0, nil
	}
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0, fmt.Errorf("docker size %q", s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("docker size %q: %w", s, err)
	}
	mult, ok := units[s[i:]]
	if !ok {
		return 0, fmt.Errorf("docker size %q: unknown unit", s)
	}
	return int64(math.Round(n * mult)), nil
}

// Usage is one row of `docker system df`.
type Usage struct {
	Type        string
	Count       int
	Size        int64
	Reclaimable int64
}

// ParseSystemDF reads `docker system df --format '{{json .}}'`.
func ParseSystemDF(text string) (map[string]Usage, error) {
	out := map[string]Usage{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct{ Type, TotalCount, Size, Reclaimable string }
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("docker system df: %w", err)
		}
		size, err := ParseSize(raw.Size)
		if err != nil {
			return nil, err
		}
		reclaimable, err := ParseSize(raw.Reclaimable)
		if err != nil {
			return nil, err
		}
		count, _ := strconv.Atoi(raw.TotalCount)
		out[raw.Type] = Usage{Type: raw.Type, Count: count, Size: size, Reclaimable: reclaimable}
	}
	return out, nil
}

// Volume is a Docker volume. unbloat never removes one of either kind: named
// volumes are where Compose projects keep databases, and an image such as the
// official Postgres one keeps its data in an anonymous volume unless it is
// given a name.
type Volume struct {
	Name      string
	Anonymous bool
}

var hexName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseVolumes reads `docker volume ls --format '{{json .}}'`. Docker labels
// anonymous volumes com.docker.volume.anonymous; a 64-character hex name is
// the fallback for engines too old to label them.
func ParseVolumes(text string) ([]Volume, error) {
	var vols []Volume
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct{ Name, Labels string }
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("docker volume ls: %w", err)
		}
		anonymous := strings.Contains(raw.Labels, "com.docker.volume.anonymous=") || hexName.MatchString(raw.Name)
		vols = append(vols, Volume{Name: raw.Name, Anonymous: anonymous})
	}
	return vols, nil
}
