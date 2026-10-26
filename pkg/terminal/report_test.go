package terminal

import (
	"github.com/charmbracelet/x/ansi"

	"bytes"
	"strings"
	"testing"
)

// What a command reports is marked and coloured by its kind: a green ✔ for
// success on stdout, a yellow ! for a warning and a red ✗ for a failure on
// stderr, "Error:" in bold red, and hints in gray. The marks stay when
// piped; the colours go, unless CLICOLOR_FORCE asks for them.
func TestReport(t *testing.T) {
	const bold, red, green, yellow, gray = "1", "31", "32", "33", "90"

	for name, tc := range map[string]struct {
		env     map[string]string
		colours bool
	}{
		"pipe":   {nil, false},
		"forced": {forcedColour, true},
	} {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			term := NewSession(Session{Out: &out, Err: &errOut, Env: tc.env})

			term.Successf("Removed %s\n", "x")
			term.Warnf("careful\n")
			term.Failf("%s: %s\n", "x", "boom")
			term.Errorf("%s\n", "boom")
			term.Hintf("Run 'fury --help' for usage.\n")

			if got := ansi.Strip(out.String()); got != "✔ Removed x\n" {
				t.Errorf("Expected the success on stdout, got %q", got)
			}
			exp := "! careful\n✗ x: boom\nError: boom\nRun 'fury --help' for usage.\n"
			if got := ansi.Strip(errOut.String()); got != exp {
				t.Errorf("Expected %q on stderr, got %q", exp, got)
			}
			all := out.String() + errOut.String()
			for _, param := range []string{bold, red, green, yellow, gray} {
				if sgr(all, param) != tc.colours {
					t.Errorf("Expected SGR %s present=%v, got %q", param, tc.colours, all)
				}
			}
		})
	}
}

// A hint of several lines is styled line by line, so none is padded to the
// width of the longest
func TestHintLines(t *testing.T) {
	var errOut bytes.Buffer
	term := NewSession(Session{Err: &errOut, Env: forcedColour})
	term.Hintf("Run:\n  %s\n", "fury --help")

	if got, exp := errOut.String(), "Run:\x1b[m\n"; !strings.Contains(got, exp) {
		t.Errorf("Expected the first line to end unpadded, got %q", got)
	}
	if got, exp := ansi.Strip(errOut.String()), "Run:\n  fury --help\n"; got != exp {
		t.Errorf("Expected %q, got %q", exp, got)
	}
}

// --quiet leaves out what went well, as it does information, but never a
// warning, a failure, or the error
func TestReportQuiet(t *testing.T) {
	var out, errOut bytes.Buffer
	term := NewSession(Session{Out: &out, Err: &errOut})
	term.SetOptions(Options{Quiet: true})

	term.Successf("Removed %s\n", "x")
	term.Warnf("careful\n")
	term.Failf("x: boom\n")
	term.Errorf("boom\n")

	if out.Len() != 0 {
		t.Errorf("Expected nothing on stdout, got %q", out.String())
	}
	if got := errOut.String(); strings.Count(got, "\n") != 3 {
		t.Errorf("Expected the warning, the failure and the error, got %q", got)
	}
}
