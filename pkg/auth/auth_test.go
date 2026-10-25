package auth

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

// fakeTerminal is what the store asks of a terminal: an environment, whether
// a person is there, and a stderr to tell of a wait on
type fakeTerminal struct {
	env         map[string]string
	interactive bool
	err         bytes.Buffer
}

func (f *fakeTerminal) Getenv(name string) string {
	return f.env[name]
}

func (f *fakeTerminal) IsInteractive() bool {
	return f.interactive
}

func (f *fakeTerminal) EPrintf(format string, a ...any) (int, error) {
	return fmt.Fprintf(&f.err, format, a...)
}

// A session held in memory is read back as saved, and gone once wiped
func TestMemory(t *testing.T) {
	store := NewMemory("", "", nil)
	expectAuth(t, store, "", "")

	saveSession(t, store)
	expectAuth(t, store, "u@example.com", "abc123")

	wipeSession(t, store)
	expectAuth(t, store, "", "")
}

// A Memory with an error fails every call, and keeps the session it has
func TestMemoryErr(t *testing.T) {
	errDown := errors.New("down")
	store := NewMemory("u@example.com", "abc123", errDown)

	if _, _, err := store.Auth(); !errors.Is(err, errDown) {
		t.Errorf("Expected Auth to fail with %v, got: %v", errDown, err)
	}
	if err := store.Append("other", "token"); !errors.Is(err, errDown) {
		t.Errorf("Expected Append to fail with %v, got: %v", errDown, err)
	}
	if err := store.Wipe(); !errors.Is(err, errDown) {
		t.Errorf("Expected Wipe to fail with %v, got: %v", errDown, err)
	}
	if store.User != "u@example.com" || store.Token != "abc123" {
		t.Errorf("Expected the session to be kept, got %+v", store)
	}
}
