package terminal

import (
	"errors"
	"strings"
	"testing"
)

// The Mode follows the session and the flags: questions need a person at
// stdin and at stderr, and asking turned on; --quiet hides what is not a
// result; bars and spinners need a person at stderr and no flag against them
func TestDecide(t *testing.T) {
	user := Caps{InTTY: true, OutTTY: true, ErrTTY: true}
	for name, tc := range map[string]struct {
		caps Caps
		opts Options
		want Mode
	}{
		"user":               {user, Options{}, Mode{Interactive: true, OutTTY: true, ShowInfo: true, ShowProgress: true}},
		"piped":              {Caps{}, Options{}, Mode{ShowInfo: true}},
		"stdout piped":       {Caps{InTTY: true, ErrTTY: true}, Options{}, Mode{Interactive: true, ShowInfo: true, ShowProgress: true}},
		"stderr piped":       {Caps{InTTY: true, OutTTY: true}, Options{}, Mode{OutTTY: true, ShowInfo: true}},
		"--yes":              {user, Options{Yes: true}, Mode{Interactive: true, OutTTY: true, ShowInfo: true, ShowProgress: true, AutoYes: true}},
		"--no-input":         {user, Options{NoInput: true}, Mode{OutTTY: true, ShowInfo: true, ShowProgress: true}},
		"--quiet":            {user, Options{Quiet: true}, Mode{Interactive: true, OutTTY: true}},
		"--no-progress":      {user, Options{NoProgress: true}, Mode{Interactive: true, OutTTY: true, ShowInfo: true}},
		"--quiet --no-input": {user, Options{Quiet: true, NoInput: true}, Mode{OutTTY: true}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := decide(tc.caps, tc.opts); got != tc.want {
				t.Errorf("Expected %+v, got %+v", tc.want, got)
			}
		})
	}
}

// A yes/no question is answered by --yes, or else by the user, if there is
// one to ask; other questions are asked only of a user. What is not a result
// is left out with --quiet, and so are bars and spinners, as with --no-progress.
func TestSetOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		opts      Options
		user      string // Answer of the one at the terminal, if any
		confirmed bool
		err       error
		asked     bool // Whether a plain question is asked
		info      bool
		progress  bool
	}{
		"asked, and confirmed": {Options{}, "Y", true, nil, true, true, true},
		"asked, and declined":  {Options{}, "N", false, nil, true, true, true},
		"no one to ask":        {Options{}, "", false, ErrNoInput, false, true, true},
		"--no-input":           {Options{NoInput: true}, "Y", false, ErrNoInput, false, true, true},
		"--yes":                {Options{Yes: true}, "N", true, nil, true, true, true},
		"--yes, no one to ask": {Options{Yes: true}, "", true, nil, false, true, true},
		"--quiet":              {Options{Quiet: true}, "Y", true, nil, true, false, false},
		"--no-progress":        {Options{NoProgress: true}, "Y", true, nil, true, true, false},
		"--quiet --no-input":   {Options{Quiet: true, NoInput: true}, "Y", false, ErrNoInput, false, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			ui := NewForTest()
			ui.SetInteractive(tc.user != "")
			ui.SetPromptResponses(map[string]string{"Sure?": tc.user, "Name": "typed"})
			ui.SetOptions(tc.opts)

			confirmed, err := ui.Confirm(t.Context(), "Sure?", false)
			if confirmed != tc.confirmed || !errors.Is(err, tc.err) {
				t.Errorf("Expected confirmed=%v and error %v, got %v and: %v", tc.confirmed, tc.err, confirmed, err)
			}

			name, err := ui.Ask(t.Context(), Prompt{Label: "Name"})
			if asked := err == nil && name == "typed"; asked != tc.asked {
				t.Errorf("Expected asked=%v, got %q and: %v", tc.asked, name, err)
			} else if !asked && !errors.Is(err, ErrNoInput) {
				t.Errorf("Expected ErrNoInput when not asked, got: %v", err)
			}

			ui.Printf("result\n")
			ui.Infof("info\n")
			ui.EPrintf("warning\n")
			exp := "result\n"
			if tc.info {
				exp += "info\n"
			}
			if out := string(ui.OutBytes()); out != exp {
				t.Errorf("Expected %q, got %q", exp, out)
			}
			if errOut := string(ui.ErrBytes()); !strings.Contains(errOut, "warning\n") {
				t.Errorf("Expected the warning, got %q", errOut)
			}

			ui.StartProgress(1024, "Uploading x ").Finish()
			ui.Spin(" Fetching ...")()
			want := 0
			if tc.progress {
				want = 1
			}
			if bars, spinners := ui.ProgressShown(); bars != want || spinners != want {
				t.Errorf("Expected %d bars and spinners, got %d and %d", want, bars, spinners)
			}
		})
	}
}
