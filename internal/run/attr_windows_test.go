//go:build windows

package run

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestChildrenGetTheirOwnProcessGroup(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	detach(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatalf("SysProcAttr = %+v", cmd.SysProcAttr)
	}
	flags := cmd.SysProcAttr.CreationFlags
	if flags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("CREATE_NEW_PROCESS_GROUP not set: flags = %#x", flags)
	}
	if flags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CREATE_NO_WINDOW not set: flags = %#x", flags)
	}
}
