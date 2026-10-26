package terminal

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"bytes"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A progress bar or spinner must never render to a non-terminal error
// stream, whether it has a file descriptor (pipe) or not (buffer)
func TestStartProgressNonTerminal(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()

	var buf bytes.Buffer
	streams := map[string]io.Writer{
		"pipe":   pw,
		"buffer": &buf,
	}

	for name, w := range streams {
		// The streams are probed, as New does for those of the process
		ui := NewSession(Session{Err: w, Caps: Caps{ErrTTY: isTerminal(w)}})
		p := ui.StartProgress(1024, "Uploading x ")

		// Reading through the proxy must be a pass-through
		body, _ := io.ReadAll(p.NewProxyReader(strings.NewReader("hello")))
		if string(body) != "hello" {
			t.Errorf("%s: proxy reader altered data: %q", name, body)
		}
		p.Finish()
		ui.Spin(" Fetching ...")()
	}

	pw.Close()
	if out, _ := io.ReadAll(pr); len(out) != 0 {
		t.Errorf("pipe received progress output: %q", out)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer received progress output: %q", buf.Bytes())
	}
}

// A bar counts what is read through it, and Finish ends its program and
// returns, however often it is called and however soon after the start
func TestBarFinish(t *testing.T) {
	b := drawingUI().StartProgress(1024, "Uploading x ").(*bar)

	body, _ := io.ReadAll(b.NewProxyReader(strings.NewReader("hello")))
	if string(body) != "hello" {
		t.Errorf("proxy reader altered data: %q", body)
	}
	if n := b.read.Load(); n != 5 {
		t.Errorf("counted %d bytes, want 5", n)
	}

	inTime(t, func() { b.Finish(); b.Finish() })
}

// A spinner's program ends when it is stopped, however often and however soon
func TestSpinStop(t *testing.T) {
	stop := drawingUI().Spin(" Waiting ...")
	inTime(t, func() { stop(); stop() })
}

// drawingUI draws its widgets, for real, on a stream that discards them
func drawingUI() *UI {
	return NewSession(Session{Err: io.Discard, Caps: Caps{ErrTTY: true}})
}

// The glyph is white, as briandowns/spinner drew it, and cycles with every
// tick; each tick asks for the next, and the view is empty once done
func TestSpinModelFrames(t *testing.T) {
	var m tea.Model = statusModel{widget: newSpinModel(" Waiting ...")}
	if got := m.View().Content; !strings.Contains(got, "\x1b[37m") {
		t.Errorf("glyph is not white: %q", got)
	}

	frames := spinSet.Frames
	for i := range len(frames) + 1 { // and back to the first
		glyph := frames[i%len(frames)]
		if got, exp := ansi.Strip(m.View().Content), glyph+" Waiting ..."; got != exp {
			t.Errorf("frame %d\n got: %q\nwant: %q", i, got, exp)
		}
		var cmd tea.Cmd
		if m, cmd = m.Update(spinner.TickMsg{}); cmd == nil {
			t.Fatalf("frame %d: no next tick scheduled", i)
		}
		// Running one tick shows the round trip works; re-arming is bubbles' own
		if i == 0 {
			if _, ok := runCmd(t, cmd).(spinner.TickMsg); !ok {
				t.Errorf("frame %d: tick did not give a tick", i)
			}
		}
	}

	assertDone(t, m)
}

// The expected frames were generated with cheggaaa/pb v3.2.1 and the legacy
// template, so a match means the bar looks as before, with no colour codes
func TestBarModelRender(t *testing.T) {
	const prefix = "Uploading x "
	tests := []struct {
		name   string
		prefix string
		width  int
		total  int64
		value  int64
		want   string
	}{
		{"zero", prefix, 40, 1024, 0, "Uploading x [                    ] 0.00%"},
		{"one byte", prefix, 40, 1024, 1, "Uploading x [⠁                   ] 0.10%"},
		{"tenth", prefix, 40, 1024, 100, "Uploading x [=⠁                  ] 9.77%"},
		{"half", prefix, 40, 1024, 512, "Uploading x [=========⠁         ] 50.00%"},
		{"full", prefix, 40, 1024, 1024, "Uploading x [=================⠁] 100.00%"},
		{"overrun", prefix, 40, 1024, 2048, "Uploading x [=================⠁] 200.00%"},
		{"emoji prefix", "⌛ ", 30, 1000, 333, "⌛ [=====⠁            ] 33.30%"},
		{"narrow brackets", prefix, 21, 1024, 512, "Uploading x [] 50.00%"},
		{"narrow bracket", prefix, 20, 1024, 512, "Uploading x [ 50.00%"},
		{"narrow cut", prefix, 15, 1024, 512, "Uploading x  50"},
		{"no total", prefix, 40, 0, 512, "Uploading x [                       ] ?%"},
		{"negative total", prefix, 40, -1, 512, "Uploading x [======================⠁] ?%"},
		{"unknown width", prefix, 0, 1024, 512,
			"Uploading x [" + strings.Repeat("=", 39) + "⠁" + strings.Repeat(" ", 39) + "] 50.00%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := frameModel(tt.prefix, tt.total, tt.value, tt.width)
			if got := m.View().Content; got != tt.want {
				t.Errorf("frame\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// While a bar is shown, the terminal is told its whole percentage, at most
// 100, or that it is busy when the total is not known; once done, nothing.
// A spinner tells it nothing.
func TestBarNative(t *testing.T) {
	for name, tc := range map[string]struct {
		total, value int64
		want         *tea.ProgressBar
	}{
		"half":     {1024, 512, &tea.ProgressBar{State: tea.ProgressBarDefault, Value: 50}},
		"rounded":  {1000, 333, &tea.ProgressBar{State: tea.ProgressBarDefault, Value: 33}},
		"overrun":  {1024, 2048, &tea.ProgressBar{State: tea.ProgressBarDefault, Value: 100}},
		"no total": {0, 512, &tea.ProgressBar{State: tea.ProgressBarIndeterminate}},
	} {
		t.Run(name, func(t *testing.T) {
			m := frameModel("", tc.total, tc.value, 40)
			if got := m.View().ProgressBar; got == nil || *got != *tc.want {
				t.Errorf("Expected %+v, got %+v", tc.want, got)
			}
			if m, _ = m.Update(doneMsg{}); m.View().ProgressBar != nil {
				t.Error("Expected no progress once done")
			}
		})
	}

	var spin tea.Model = statusModel{widget: newSpinModel(" Waiting ...")}
	if got := spin.View().ProgressBar; got != nil {
		t.Errorf("Expected a spinner to tell the terminal nothing, got %+v", got)
	}
}

// A bar's program, run as it is drawn, shows the bar at the width of the
// terminal and tells the terminal its progress; once done, it wipes its
// line and resets that progress, leaving nothing behind
func TestBarProgram(t *testing.T) {
	read := new(atomic.Int64)
	read.Store(512)
	tm := teatest.NewTestModel(t, statusModel{widget: newBarModel("Uploading x ", 1024, read)},
		teatest.WithInitialTermSize(40, 1))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(ansi.Strip(string(b)), "] 50.00%") && strings.Contains(string(b), ansi.SetProgressBar(50))
	}, teatest.WithDuration(5*time.Second))

	tm.Send(doneMsg{})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))
	if got := final.View().Content; got != "" {
		t.Errorf("Expected the line wiped, got %q", got)
	}
	if out, _ := io.ReadAll(tm.FinalOutput(t)); !strings.Contains(string(out), ansi.ResetProgressBar) {
		t.Errorf("Expected the terminal's progress reset, got %q", out)
	}
}

// The tip cycles with every tick, each tick asks for the next, and the view
// is empty once done
func TestBarModelFrames(t *testing.T) {
	m := frameModel("", 1024, 512, 20)

	want := []string{"⠁", "⠂", "⠄", "⠂", "⠁"}
	for i, tip := range want {
		if got, exp := m.View().Content, "[====="+tip+"     ] 50.00%"; got != exp {
			t.Errorf("frame %d\n got: %q\nwant: %q", i, got, exp)
		}
		var cmd tea.Cmd
		if m, cmd = m.Update(tickMsg{}); cmd == nil {
			t.Fatalf("frame %d: no next tick scheduled", i)
		}
		// Running the first two ticks shows that each one is a timer of
		// its own; running the rest would only add waiting
		if i < 2 {
			if msg := runCmd(t, cmd); msg != (tickMsg{}) {
				t.Errorf("frame %d: tick gave %#v", i, msg)
			}
		}
	}

	assertDone(t, m)
}

// assertDone tells the model it is done, which must quit with an empty view
func assertDone(t *testing.T, m tea.Model) {
	t.Helper()
	m, cmd := m.Update(doneMsg{})
	if cmd == nil {
		t.Fatal("done: no command")
	}
	if _, ok := runCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Errorf("done: command is not Quit")
	}
	if got := m.View().Content; got != "" {
		t.Errorf("done: view is not empty: %q", got)
	}
}

// frameModel is a bar's model that knows the terminal width and the bytes read
func frameModel(prefix string, total, value int64, width int) tea.Model {
	read := new(atomic.Int64)
	read.Store(value)
	var m tea.Model = statusModel{widget: newBarModel(prefix, total, read)}
	m, _ = m.Update(tea.WindowSizeMsg{Width: width})
	return m
}

// runCmd runs a command and returns its message
func runCmd(t *testing.T, cmd tea.Cmd) (msg tea.Msg) {
	t.Helper()
	inTime(t, func() { msg = cmd() })
	return msg
}

// inTime calls fn, which must return in good time, or fails the test
func inTime(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not return in time")
	}
}
