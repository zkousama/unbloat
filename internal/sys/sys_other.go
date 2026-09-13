//go:build !windows

package sys

import "errors"

var errNotWindows = errors.New("unbloat runs on Windows")

// Machine is a stub so the rest of the code builds and tests elsewhere.
type Machine struct{}

func (Machine) Elevated() bool                     { return false }
func (Machine) RelaunchElevated() error            { return errNotWindows }
func (Machine) Power() Power                       { return Power{} }
func (Machine) Locked(string) (bool, error)        { return false, errNotWindows }
func (Machine) Ancestors() []string                { return nil }
func (Machine) Space(string) (int64, int64, error) { return 0, 0, errNotWindows }
