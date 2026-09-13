package wsl

import (
	"os"
	"reflect"
	"testing"
)

func TestDecodeReadsTheRecordedUTF16Output(t *testing.T) {
	b, err := os.ReadFile("testdata/list-quiet.utf16")
	if err != nil {
		t.Fatal(err)
	}
	got := ParseNames(Decode(b))
	want := []string{"Ubuntu", "docker-desktop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeReadsTheRecordedRunningList(t *testing.T) {
	b, err := os.ReadFile("testdata/list-running.utf16")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseNames(Decode(b)); !reflect.DeepEqual(got, []string{"Ubuntu"}) {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeStripsAByteOrderMark(t *testing.T) {
	b := []byte{0xFF, 0xFE, 'U', 0, 'b', 0, '\r', 0, '\n', 0}
	if got := Decode(b); got != "Ub\n" {
		t.Fatalf("got %q", got)
	}
}

// Output from a program inside a distro is UTF-8, not UTF-16, and has to come
// through untouched.
func TestDecodePassesUTF8Through(t *testing.T) {
	if got := Decode([]byte("Used\r\n21896672\r\n")); got != "Used\n21896672\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeHandlesAnOddByteAtTheEnd(t *testing.T) {
	b := []byte{'U', 0, 'b', 0, 'x'}
	if got := Decode(b); got != "Ub" {
		t.Fatalf("got %q", got)
	}
}

func TestParseNamesSkipsBlankLines(t *testing.T) {
	got := ParseNames("\nUbuntu\n\n  Debian  \n")
	if !reflect.DeepEqual(got, []string{"Ubuntu", "Debian"}) {
		t.Fatalf("got %q", got)
	}
}
