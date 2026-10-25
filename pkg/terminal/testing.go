package terminal

import (
	"github.com/manifoldco/promptui"

	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// testLocation is the zone of a test terminal: one of its own, so that no
// test depends on the zone of the machine, and not UTC, which the API
// sends, so that a time shown unconverted is caught
var testLocation = time.FixedZone("UTC+9", 9*60*60)

type TestTerm interface {
	ErrBytes() []byte
	OutBytes() []byte
	Terminal
}

func NewForTest() *testTerm {
	streams := []*bytes.Buffer{{}, {}, {}}
	return &testTerm{
		interactive: true,
		prompts:     map[string]string{},
		streams:     streams,
		term: &term{
			ioErr:  writeCloser{streams[0]},
			ioOut:  writeCloser{streams[1]},
			ioIn:   io.NopCloser(streams[2]),
			getenv: os.Getenv,
			loc:    testLocation,
		},
	}
}

type testTerm struct {
	interactive    bool
	outTTY         bool
	prompts        map[string]string
	streams        []*bytes.Buffer
	bars, spinners int
	*term
}

// Tests stand in for a user at the terminal, unless told otherwise
func (tt testTerm) IsInteractive() bool {
	return tt.interactive
}

// SetInteractive(false) simulates a run without a terminal (pipe, CI)
func (tt *testTerm) SetInteractive(interactive bool) {
	tt.interactive = interactive
}

// Tests read the output as a program would, not a person, unless told otherwise
func (tt testTerm) IsOutTTY() bool {
	return tt.outTTY
}

// SetOutTTY(true) simulates output that is read at a terminal
func (tt *testTerm) SetOutTTY(outTTY bool) {
	tt.outTTY = outTTY
}

// SetTimeLocation shows times in the given zone, as for a session of a
// host, rather than in testLocation
func (tt *testTerm) SetTimeLocation(loc *time.Location) {
	tt.loc = loc
}

// SetEnv gives the terminal an environment of its own, as a host would,
// rather than that of the process, as tests set it; nil is that again
func (tt *testTerm) SetEnv(env map[string]string) {
	tt.getenv = os.Getenv
	if env != nil {
		tt.getenv = envGetenv(env)
	}
}

func (tt testTerm) ErrBytes() []byte {
	return tt.streams[0].Bytes()
}

func (tt testTerm) OutBytes() []byte {
	return tt.streams[1].Bytes()
}

func (tt testTerm) InWrite(b []byte) (int, error) {
	return tt.streams[2].Write(b)
}

// OnOutput calls fn each time that there is a write to Stdout
func (tt *testTerm) OnOutput(fn func()) {
	tt.ioOut = writeCloser{writerFunc(func(b []byte) (int, error) {
		defer fn()
		return tt.streams[1].Write(b)
	})}
}

// OnErrOutput calls fn each time that there is a write to Stderr
func (tt *testTerm) OnErrOutput(fn func()) {
	tt.ioErr = writeCloser{writerFunc(func(b []byte) (int, error) {
		defer fn()
		return tt.streams[0].Write(b)
	})}
}

// Handle PromptUI to avoid messing with Readline
func (tt *testTerm) SetPromptResponses(p map[string]string) {
	tt.prompts = p
}

// StartProgress shows no bar, but counts it
func (tt *testTerm) StartProgress(int64, string) Progress {
	tt.bars++
	return noProgress{}
}

// Spin shows no spinner, but counts it
func (tt *testTerm) Spin(string) func() {
	tt.spinners++
	return func() {}
}

// ProgressShown counts the progress bars and spinners that were asked for
func (tt testTerm) ProgressShown() (bars, spinners int) {
	return tt.bars, tt.spinners
}

// OpenBrowser opens nothing, as a test has no browser
func (tt *testTerm) OpenBrowser(context.Context, string) bool {
	return false
}

// Confirm is answered as any other prompt, and declined unless by a yes
func (tt testTerm) Confirm(label string) (bool, error) {
	answer, err := tt.RunPrompt(&promptui.Prompt{Label: label})
	if err == nil && !strings.EqualFold(answer, "y") {
		err = promptui.ErrAbort
	}
	return confirmed(err)
}

func (tt testTerm) RunPrompt(p *promptui.Prompt) (string, error) {
	if l, ok := p.Label.(string); ok {
		if out, ok := tt.prompts[l]; ok {
			switch out {
			case "ABORT":
				return "", promptui.ErrAbort
			case "INTERRUPT":
				return "", promptui.ErrInterrupt
			case "EOF":
				return "", promptui.ErrEOF
			}
			return out, nil
		}
	}
	return "", io.EOF
}

// Equivalent to http.HandlerFunc for writers
type writerFunc func([]byte) (int, error)

func (fn writerFunc) Write(b []byte) (int, error) {
	return fn(b)
}
