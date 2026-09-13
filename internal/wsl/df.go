package wsl

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseDfUsed reads the Used column of `df -Pk` and returns bytes. -P keeps
// each filesystem on one line under both GNU df and busybox, which is what
// Docker's distro has.
func ParseDfUsed(text string) (int64, error) {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return 0, fmt.Errorf("df printed no data line: %q", text)
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 6 {
		return 0, fmt.Errorf("df data line has %d fields: %q", len(fields), lines[len(lines)-1])
	}
	kb, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("df used column %q: %w", fields[2], err)
	}
	return kb * 1024, nil
}
