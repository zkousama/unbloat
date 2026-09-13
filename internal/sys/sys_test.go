package sys

import "testing"

func TestPowerFrom(t *testing.T) {
	for name, tc := range map[string]struct {
		ac, flag, pct byte
		want          Power
	}{
		"plugged in":                          {1, 1, 80, Power{OnBattery: false, Percent: 80, Known: true}},
		"on battery":                          {0, 0, 42, Power{OnBattery: true, Percent: 42, Known: true}},
		"percent unknown":                     {0, 0, 255, Power{OnBattery: true}},
		"a desktop, no battery":               {1, 128, 255, Power{OnBattery: false, Percent: 100, Known: true}},
		"line status unknown, charged":        {255, 0, 80, Power{OnBattery: true, Percent: 80, Known: true}},
		"line status unknown, charge unknown": {255, 0, 255, Power{OnBattery: true}},
	} {
		if got := PowerFrom(tc.ac, tc.flag, tc.pct); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestFakeFactsSatisfiesFacts(t *testing.T) {
	var f Facts = &FakeFacts{LockedPaths: map[string]bool{`C:\x.vhdx`: true}}
	if locked, _ := f.Locked(`C:\x.vhdx`); !locked {
		t.Fatal("want locked")
	}
	if err := f.RelaunchElevated(); err != nil || !f.(*FakeFacts).Relaunched {
		t.Fatal("relaunch not recorded")
	}
}
