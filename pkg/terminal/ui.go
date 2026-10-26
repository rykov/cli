package terminal

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"
	"github.com/gemfury/cli/pkg/browser"

	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"
)

// Session is what a host gives the UI of a session: its streams, its
// environment, its zone, and which of its streams a person is at. What is
// left out is empty, as the fields say, so that nothing is asked and output
// is as when piped.
type Session struct {
	In       io.Reader
	Out, Err io.Writer
	Env      map[string]string // Of the session: nil is an empty one, never the host's
	Location *time.Location    // Zone that times are shown in: nil is UTC
	Caps     Caps              // Streams a person is at: none, unless the host says so
}

// UI is how a command shows and asks: it prints on the streams of its
// session, draws bars and spinners, and asks questions, as its Mode allows.
// A host makes one with NewSession and gives it to cli.NewContext.
type UI struct {
	session Session
	opts    Options // As given to SetOptions
	env     environ // Where Getenv reads, and what programs are given
	asker   asker   // Asks the questions the Mode allows
	drawer  drawer  // Draws the widgets the Mode allows

	outProfile, errProfile *colorprofile.Profile // Of the streams, once detected (profile)

	openBrowser func(context.Context, string) bool
	test        *testHooks // Of a UI made by NewForTest; nil otherwise. No method a command calls reads it.
}

// New is the UI of the process: its streams, its environment, and its zone
func New() *UI {
	return newUI(Session{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		Location: time.Local,
		Caps: Caps{
			InTTY:  isTerminal(os.Stdin),
			OutTTY: isTerminal(os.Stdout),
			ErrTTY: isTerminal(os.Stderr),
		},
	}, processEnv())
}

// NewSession is the UI of a session of a host
func NewSession(s Session) *UI {
	if s.In == nil {
		s.In = strings.NewReader("")
	}
	if s.Out == nil {
		s.Out = io.Discard
	}
	if s.Err == nil {
		s.Err = io.Discard
	}
	if s.Location == nil {
		s.Location = time.UTC
	}
	s.Env = maps.Clone(s.Env) // The host may change its own
	return newUI(s, mapEnv(s.Env))
}

// newUI is a UI that asks with forms and draws with programs on the streams
// of s, and reads env
func newUI(s Session, env environ) *UI {
	u := &UI{session: s, env: env, openBrowser: browser.Open}
	u.asker, u.drawer = forms{u}, programs{u}
	return u
}

// SetOptions gives the UI the command's global flags, for its Mode. A UI is
// usable before this, as made: nothing is turned off.
func (u *UI) SetOptions(o Options) {
	u.opts = o
}

// Mode is that of the session's Caps and the Options of SetOptions (decide)
func (u *UI) Mode() Mode {
	return decide(u.session.Caps, u.opts)
}

// Printf prints the result of a command
func (u *UI) Printf(f string, a ...any) (int, error) {
	return fmt.Fprintf(u.session.Out, f, a...)
}

// Infof prints what is not the result of a command: a banner, the status
// of a change, or that nothing was found. With --quiet it writes nothing,
// and returns 0, which tells a caller that it was left out.
func (u *UI) Infof(f string, a ...any) (int, error) {
	if !u.Mode().ShowInfo {
		return 0, nil
	}
	return fmt.Fprintf(u.session.Out, f, a...)
}

// EPrintf prints to the error stream as given, with no mark, unlike Warnf
// and Failf
func (u *UI) EPrintf(f string, a ...any) (int, error) {
	return fmt.Fprintf(u.session.Err, f, a...)
}

// IOErr, IOOut and IOIn are the streams of the session, as given, for what
// writes or reads them itself: a JSON encoder, a table, Cobra's help
func (u *UI) IOErr() io.Writer {
	return u.session.Err
}

func (u *UI) IOOut() io.Writer {
	return u.session.Out
}

func (u *UI) IOIn() io.Reader {
	return u.session.In
}

// IsInteractive is Mode().Interactive
func (u *UI) IsInteractive() bool {
	return u.Mode().Interactive
}

// IsOutTTY is Mode().OutTTY
func (u *UI) IsOutTTY() bool {
	return u.Mode().OutTTY
}

// TimeLocation is the zone that times are shown in. It is never nil.
func (u *UI) TimeLocation() *time.Location {
	return u.session.Location
}

// Getenv reads the environment of the session, which need not be that of
// the process, as for each session of a host
func (u *UI) Getenv(name string) string {
	return u.env.get(name)
}

// OpenBrowser opens url in the browser of the process and reports whether
// that worked; a test UI has no browser, and reports that it did not
func (u *UI) OpenBrowser(ctx context.Context, url string) bool {
	return u.openBrowser(ctx, url)
}

// programOptions are those of every program the UI runs, on stderr: the
// environment of the session, the colours of stderr as detected once for
// what is printed there (profile), rather than again by each program, and
// no signal handler of its own, as the process has one
func (u *UI) programOptions() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithEnvironment(u.env.list()),
		tea.WithColorProfile(u.profile(&u.errProfile, u.session.Err)),
		tea.WithoutSignalHandler(),
	}
}

// environ is an environment to read: that of the process, live, or the
// map of a session
type environ struct {
	get  func(name string) string
	list func() []string // As the "NAME=value" list that programs read
}

// processEnv is the environment of the process, live
func processEnv() environ {
	return environ{get: os.Getenv, list: os.Environ}
}

// mapEnv is the environment of a session, as the host gave it
func mapEnv(m map[string]string) environ {
	return environ{
		get: func(name string) string { return m[name] },
		list: func() []string {
			list := make([]string, 0, len(m))
			for name, value := range m {
				list = append(list, name+"="+value)
			}
			return list
		},
	}
}

// isTerminal reports whether a stream (reader or writer) is backed by an
// interactive terminal. Anything without a file descriptor, such as the
// buffers used in tests, is not.
func isTerminal(stream any) bool {
	_, ok := terminalFd(stream)
	return ok
}

// terminalFd is the file descriptor of a stream that isTerminal
func terminalFd(stream any) (uintptr, bool) {
	f, ok := stream.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	fd := f.Fd()
	return fd, xterm.IsTerminal(fd)
}
