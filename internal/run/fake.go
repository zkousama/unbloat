package run

import (
	"context"
	"fmt"
	"sync"
)

// Fake replays recorded output. A command with nothing recorded fails the
// caller, so a test cannot pass by a code path quietly running something it
// never set up.
type Fake struct {
	mu        sync.Mutex
	responses map[string]fakeResponse
	calls     []string
}

type fakeResponse struct {
	res Result
	err error
}

func NewFake() *Fake {
	return &Fake{responses: map[string]fakeResponse{}}
}

// On records what a command returns.
func (f *Fake) On(res Result, name string, args ...string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[Key(name, args...)] = fakeResponse{res: res}
	return f
}

// Fails records a command that cannot start, as a missing executable cannot.
func (f *Fake) Fails(err error, name string, args ...string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[Key(name, args...)] = fakeResponse{res: Result{Code: -1}, err: err}
	return f
}

func (f *Fake) Run(_ context.Context, name string, args ...string) (Result, error) {
	key := Key(name, args...)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	r, ok := f.responses[key]
	if !ok {
		return Result{Code: -1}, fmt.Errorf("fake runner: nothing recorded for %q", key)
	}
	return r.res, r.err
}

// Calls lists every command run, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Out is a successful result with the given stdout.
func Out(stdout string) Result { return Result{Stdout: []byte(stdout)} }

// Exit is a result with the given exit code and stdout.
func Exit(code int, stdout string) Result { return Result{Code: code, Stdout: []byte(stdout)} }
