//go:build !windows

package run

import "os/exec"

func detach(*exec.Cmd) {}
