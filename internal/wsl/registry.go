package wsl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RegistryScript prints each distro's registration as UTF-8 JSON. reg.exe is
// not used: it writes in the console's legacy code page, which mangles a user
// name with an accent and so the path to the disk.
const RegistryScript = `[Console]::OutputEncoding = [Text.Encoding]::UTF8; ` +
	`@(Get-ChildItem 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Lxss' | ` +
	`ForEach-Object { Get-ItemProperty -LiteralPath $_.PSPath | ` +
	`Select-Object DistributionName, BasePath, VhdFileName, Version }) | ConvertTo-Json -Compress`

// Registration is one distro as the registry knows it.
type Registration struct {
	Name     string
	Version  int
	BasePath string
	VhdFile  string
}

// ParseRegistrations reads RegistryScript's output. PowerShell prints an
// object rather than an array when there is exactly one distro, and nothing at
// all when there are none.
func ParseRegistrations(text string) ([]Registration, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	if strings.HasPrefix(text, "{") {
		text = "[" + text + "]"
	}
	var raw []struct {
		DistributionName string
		BasePath         string
		VhdFileName      *string
		Version          int
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("reading WSL registrations: %w", err)
	}
	regs := make([]Registration, 0, len(raw))
	for _, r := range raw {
		base := strings.TrimPrefix(r.BasePath, `\\?\`)
		vhd := "ext4.vhdx"
		if r.VhdFileName != nil && *r.VhdFileName != "" {
			vhd = *r.VhdFileName
		}
		regs = append(regs, Registration{
			Name:     r.DistributionName,
			Version:  r.Version,
			BasePath: base,
			VhdFile:  strings.TrimRight(base, `\`) + `\` + vhd,
		})
	}
	return regs, nil
}
