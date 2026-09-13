// Package sys reports facts about the machine that are not commands.
package sys

// Power is the machine's power state.
type Power struct {
	OnBattery bool
	Percent   int
	Known     bool // false when Windows reports no percentage
}

// PowerFrom reads GetSystemPowerStatus. An AC line status of 0 is battery, 1
// is mains and 255 unknown. A battery flag of 128 means there is no battery,
// which is a desktop and always safe. A percentage of 255 is unknown.
func PowerFrom(acLine, batteryFlag, percent byte) Power {
	if batteryFlag == 128 {
		return Power{OnBattery: false, Percent: 100, Known: true}
	}
	p := Power{OnBattery: acLine == 0}
	if percent <= 100 {
		p.Percent = int(percent)
		p.Known = true
	}
	return p
}

// Facts is what unbloat needs to know about the machine.
type Facts interface {
	Elevated() bool
	RelaunchElevated() error
	Power() Power
	Locked(path string) (bool, error)
	Ancestors() []string // process names from the parent upwards
	Space(path string) (free, total int64, err error)
}
