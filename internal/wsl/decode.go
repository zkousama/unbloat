// Package wsl talks to WSL through wsl.exe and the registry.
package wsl

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// Decode returns wsl.exe's output as text.
//
// wsl.exe writes its own messages as UTF-16LE with no byte order mark when
// its output is a pipe, and setting WSL_UTF8 is not reliable enough to depend
// on. A program run inside a distro writes UTF-8. Both come through here.
func Decode(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return normalize(decodeUTF16LE(b[2:]))
	}
	if looksUTF16LE(b) {
		return normalize(decodeUTF16LE(b))
	}
	return normalize(string(b))
}

// looksUTF16LE reports whether most odd bytes are zero, which is what ASCII
// text looks like in UTF-16LE and almost never what UTF-8 looks like.
func looksUTF16LE(b []byte) bool {
	if len(b) < 2 {
		return false
	}
	odd, zeros := 0, 0
	for i := 1; i < len(b); i += 2 {
		odd++
		if b[i] == 0 {
			zeros++
		}
	}
	return zeros*2 >= odd
}

func decodeUTF16LE(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(units))
}

func normalize(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// ParseNames reads one distro name per line, as `wsl.exe --list --quiet`
// prints them. Distro names cannot contain spaces.
func ParseNames(text string) []string {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}
