package terminal

import (
	"bytes"
	"testing"
	"time"
)

// A terminal of a session has the streams, the environment, and the zone
// that the host gives it, and never those of the process
func TestNewSession(t *testing.T) {
	t.Setenv("FURY_TOKEN", "process-token")

	var out, errOut bytes.Buffer
	zone := time.FixedZone("UTC-5", -5*60*60)
	env := map[string]string{"FURY_ACCOUNT": "session-org"}
	term := NewSession(Session{Out: &out, Err: &errOut, Env: env, Location: zone})
	env["FURY_ACCOUNT"] = "changed-by-host" // The session has its own copy

	if got := term.Getenv("FURY_ACCOUNT"); got != "session-org" {
		t.Errorf("Expected the account of the session, got %q", got)
	}
	if got := term.Getenv("FURY_TOKEN"); got != "" {
		t.Errorf("Expected no token, not that of the process, got %q", got)
	}
	if got := term.TimeLocation(); got != zone {
		t.Errorf("Expected the zone of the session, got %v", got)
	}
	if term.IsInteractive() || term.IsOutTTY() {
		t.Error("Expected streams without a terminal: not interactive, not a TTY")
	}

	term.Printf("out")
	term.EPrintf("err")
	if out.String() != "out" || errOut.String() != "err" {
		t.Errorf("Expected output on the streams of the session, got %q and %q", out.String(), errOut.String())
	}
}

// What a host leaves out is empty, never taken from the process
func TestNewSessionDefaults(t *testing.T) {
	t.Setenv("FURY_TOKEN", "process-token")
	term := NewSession(Session{})

	if got := term.Getenv("FURY_TOKEN"); got != "" {
		t.Errorf("Expected an empty environment, got %q", got)
	}
	if got := term.TimeLocation(); got != time.UTC {
		t.Errorf("Expected UTC, got %v", got)
	}
	if _, err := term.Printf("out"); err != nil {
		t.Errorf("Expected output to be discarded, got: %v", err)
	}
	if n, _ := term.IOIn().Read(make([]byte, 1)); n != 0 {
		t.Errorf("Expected no input, got %d bytes", n)
	}
}
