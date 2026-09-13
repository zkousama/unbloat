//go:build windows

package run

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach starts the child in its own process group with its own hidden
// console. A keyboard Ctrl+C or Ctrl+Break reaches every process attached to
// the console it was pressed in, whatever its process group; only giving the
// child CREATE_NO_WINDOW gives it a console of its own, so neither signal
// reaches it. Its output still arrives through the pipes. An interrupt is
// unbloat's to handle: it must not reach a diskpart that is halfway through
// compacting.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
}
