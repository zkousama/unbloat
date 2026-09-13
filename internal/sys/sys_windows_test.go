//go:build windows

package sys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockedSeesAFileThatIsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.vhdx")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := Machine{}
	if locked, err := m.Locked(path); err != nil || locked {
		t.Fatalf("closed file: locked %v, %v", locked, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if locked, err := m.Locked(path); err != nil || !locked {
		t.Fatalf("open file: locked %v, %v", locked, err)
	}
}

func TestSpaceAndAncestorsAnswer(t *testing.T) {
	m := Machine{}
	free, total, err := m.Space(t.TempDir())
	if err != nil || total <= 0 || free < 0 || free > total {
		t.Fatalf("free %d total %d err %v", free, total, err)
	}
	if len(m.Ancestors()) == 0 {
		t.Fatal("a test process has at least one ancestor")
	}
}
