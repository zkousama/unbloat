//go:build windows

package sys

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Machine is the real Windows machine.
type Machine struct{}

func (Machine) Elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RelaunchElevated starts this program again through UAC. The caller exits.
func (Machine) RelaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(os.Args[1:]))
	return windows.ShellExecute(0, verb, file, args, nil, windows.SW_NORMAL)
}

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func (Machine) Power() Power {
	var s systemPowerStatus
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return Power{}
	}
	return PowerFrom(s.ACLineStatus, s.BatteryFlag, s.BatteryLifePercent)
}

// Locked reports whether another process has the file open, by asking for it
// with no sharing at all.
func (Machine) Locked(path string) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return true, nil
		}
		return false, err
	}
	windows.CloseHandle(h)
	return false, nil
}

func (Machine) Ancestors() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	parent := map[uint32]uint32{}
	name := map[uint32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parent[e.ProcessID] = e.ParentProcessID
		name[e.ProcessID] = windows.UTF16ToString(e.ExeFile[:])
	}

	var chain []string
	seen := map[uint32]bool{}
	for pid := parent[uint32(os.Getpid())]; pid != 0 && !seen[pid]; pid = parent[pid] {
		seen[pid] = true
		n, ok := name[pid]
		if !ok {
			break
		}
		chain = append(chain, n)
	}
	return chain
}

func (Machine) Space(path string) (int64, int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(free), int64(total), nil
}
