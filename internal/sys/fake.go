package sys

// FakeFacts is a machine for tests.
type FakeFacts struct {
	IsElevated  bool
	PowerStatus Power
	LockedPaths map[string]bool
	Chain       []string
	Free, Total int64
	Relaunched  bool
}

func (f *FakeFacts) Elevated() bool          { return f.IsElevated }
func (f *FakeFacts) RelaunchElevated() error { f.Relaunched = true; return nil }
func (f *FakeFacts) Power() Power            { return f.PowerStatus }
func (f *FakeFacts) Locked(path string) (bool, error) {
	return f.LockedPaths[path], nil
}
func (f *FakeFacts) Ancestors() []string                { return f.Chain }
func (f *FakeFacts) Space(string) (int64, int64, error) { return f.Free, f.Total, nil }
