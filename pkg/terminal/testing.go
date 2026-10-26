package terminal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

// testLocation is the zone of a test UI: one of its own, so that no test
// depends on the zone of the machine, and not UTC, which the API sends, so
// that a time shown unconverted is caught
var testLocation = time.FixedZone("UTC+9", 9*60*60)

// testHooks are the stream buffers, the script, and the counter of a UI
// made by NewForTest: what its knobs reach
type testHooks struct {
	in, out, err *bytes.Buffer
	script       *script
	counter      *counter
}

// NewForTest is a UI for tests: its streams are buffers, there is a user
// at a terminal to answer, as scripted by SetPromptResponses, and nothing
// is drawn, but counted. Its environment is that of the process, as a test
// sets it, unless SetEnv gives it one.
func NewForTest() *UI {
	h := &testHooks{
		in: &bytes.Buffer{}, out: &bytes.Buffer{}, err: &bytes.Buffer{},
		script:  &script{answers: map[string]string{}},
		counter: &counter{},
	}
	u := newUI(Session{
		In: h.in, Out: h.out, Err: h.err,
		Location: testLocation,
		Caps:     Caps{InTTY: true, ErrTTY: true},
	}, processEnv())
	u.asker, u.drawer = h.script, h.counter
	u.openBrowser = func(context.Context, string) bool { return false } // A test has no browser
	u.test = h
	return u
}

// hooks are those of a UI made by NewForTest
func (u *UI) hooks() *testHooks {
	if u.test == nil {
		panic("terminal: not a UI made by NewForTest")
	}
	return u.test
}

// script is the asker of a test UI: it answers by the label of the question
type script struct {
	answers map[string]string
}

// answer is the scripted answer to a question, by its label. One without
// an answer fails, as the test did not expect it. "INTERRUPT" and "EOF"
// interrupt, as Ctrl-C does, and so does the end of ctx, as at a real form.
func (s *script) answer(ctx context.Context, label string) (string, error) {
	if ctx.Err() != nil {
		return "", context.Canceled
	}
	out, ok := s.answers[label]
	if !ok {
		return "", fmt.Errorf("terminal: no answer scripted for %q", label)
	}
	if out == "INTERRUPT" || out == "EOF" {
		return "", context.Canceled
	}
	return out, nil
}

func (s *script) ask(ctx context.Context, p Prompt) (string, error) {
	answer, err := s.answer(ctx, p.Label)
	if answer == "ABORT" { // A form has no key to back out with, only Ctrl-C
		return "", fmt.Errorf("terminal: no backing out of %q; script INTERRUPT", p.Label)
	}
	return answer, err
}

// confirm is answered as typed at the real form, with "" for the default;
// "ABORT" backs out, which declines
func (s *script) confirm(ctx context.Context, label string, dflt bool) (bool, error) {
	answer, err := s.answer(ctx, label)
	if err != nil || answer == "ABORT" {
		return false, err
	}
	yes, ok := yesOrNo(answer, dflt)
	if !ok {
		return false, fmt.Errorf("terminal: %q is not an answer to %q", answer, label)
	}
	return yes, nil
}

// counter is the drawer of a test UI: nothing is drawn, but counted
type counter struct {
	bars, spinners int
}

func (c *counter) draw(w widget) (stop func()) {
	switch w.(type) {
	case barModel:
		c.bars++
	case spinModel:
		c.spinners++
	}
	return func() {}
}

// SetInteractive(false) simulates stdin that is not a terminal (a pipe, CI),
// so there is no one to answer
func (u *UI) SetInteractive(interactive bool) {
	u.hooks()
	u.session.Caps.InTTY = interactive
}

// SetOutTTY(true) simulates output that is read at a terminal
func (u *UI) SetOutTTY(outTTY bool) {
	u.hooks()
	u.session.Caps.OutTTY = outTTY
}

// SetTimeLocation shows times in the given zone, as for a session of a
// host, rather than in testLocation
func (u *UI) SetTimeLocation(loc *time.Location) {
	u.hooks()
	u.session.Location = loc
}

// SetEnv gives the UI an environment of its own, as a host would, rather
// than that of the process, as tests set it; nil is that again
func (u *UI) SetEnv(env map[string]string) {
	u.hooks()
	u.env = processEnv()
	if env != nil {
		u.env = mapEnv(env)
	}
	u.forgetProfiles()
}

// SetPromptResponses scripts the answers to questions, by their labels
func (u *UI) SetPromptResponses(answers map[string]string) {
	u.hooks().script.answers = answers
}

func (u *UI) ErrBytes() []byte {
	return u.hooks().err.Bytes()
}

func (u *UI) OutBytes() []byte {
	return u.hooks().out.Bytes()
}

func (u *UI) InWrite(b []byte) (int, error) {
	return u.hooks().in.Write(b)
}

// OnOutput calls fn each time that there is a write to Stdout
func (u *UI) OnOutput(fn func()) {
	u.hooks()
	u.session.Out = tapped(u.session.Out, fn)
	u.forgetProfiles()
}

// OnErrOutput calls fn each time that there is a write to Stderr
func (u *UI) OnErrOutput(fn func()) {
	u.hooks()
	u.session.Err = tapped(u.session.Err, fn)
	u.forgetProfiles()
}

// tapped is w with fn called after each write to it
func tapped(w io.Writer, fn func()) io.Writer {
	return writerFunc(func(b []byte) (int, error) {
		defer fn()
		return w.Write(b)
	})
}

// ProgressShown counts the progress bars and spinners that were asked for
func (u *UI) ProgressShown() (bars, spinners int) {
	c := u.hooks().counter
	return c.bars, c.spinners
}

// Equivalent to http.HandlerFunc for writers
type writerFunc func([]byte) (int, error)

func (fn writerFunc) Write(b []byte) (int, error) {
	return fn(b)
}
