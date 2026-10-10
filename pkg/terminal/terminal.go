package terminal

import (
	"github.com/gemfury/cli/pkg/browser"
	"github.com/manifoldco/promptui"

	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"
)

type Terminal interface {
	StartProgress(int64, string) Progress
	RunPrompt(*promptui.Prompt) (string, error)
	Confirm(label string) (bool, error)

	// Printf prints the result of a command, or what the user is asked
	Printf(string, ...any) (int, error)

	// Infof prints what is not the result of a command: a banner, the status
	// of a change, or that nothing was found. With --quiet it writes nothing,
	// and returns 0, which tells a caller that it was left out.
	Infof(string, ...any) (int, error)

	// EPrintf prints a warning, or the failure of an item, to the error stream
	EPrintf(string, ...any) (int, error)

	Spin(suffix string) func()
	OpenBrowser(context.Context, string) bool

	// IsInteractive reports whether there is a user to answer prompts: Stdin
	// is a terminal rather than a pipe, a file, or closed, and asking is
	// not turned off (see Unattended)
	IsInteractive() bool

	// IsOutTTY reports whether Stdout is a terminal rather than a pipe or
	// a file: whether a person reads what is printed, as it is printed
	IsOutTTY() bool

	// TimeLocation is the zone that times are shown in: that of the
	// process, or of each session of a host. It is never nil.
	TimeLocation() *time.Location

	// Getenv reads the environment of the session, which need not be
	// that of the process, as for each session of a host
	Getenv(name string) string

	IOIn() io.ReadCloser
	IOErr() io.Writer
	IOOut() io.Writer
}

// New is the terminal of the process: its streams, its environment, and
// its zone
func New() Terminal {
	return &term{
		ioErr:  os.Stderr,
		ioOut:  os.Stdout,
		ioIn:   os.Stdin,
		getenv: os.Getenv,
		loc:    time.Local,
	}
}

// Session is what a host gives the terminal of a session: its streams,
// its environment, and its zone
type Session struct {
	In       io.Reader
	Out, Err io.Writer
	Env      map[string]string // Of the session: nil is an empty one, never the host's
	Location *time.Location    // Zone that times are shown in: nil is UTC
}

// NewSession is the terminal of a session of a host. Whatever its streams,
// it is not interactive and its output is not a TTY: nothing is asked, and
// output is as when piped. A host that wants more, such as output for a
// person, embeds it and overrides IsOutTTY.
func NewSession(s Session) Terminal {
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

	return &term{
		ioErr:  writeCloser{s.Err},
		ioOut:  writeCloser{s.Out},
		ioIn:   io.NopCloser(s.In),
		getenv: envGetenv(s.Env),
		loc:    s.Location,
	}
}

// envGetenv reads a copy of env, so that the caller may change its own
func envGetenv(env map[string]string) func(string) string {
	env = maps.Clone(env)
	return func(name string) string { return env[name] }
}

type term struct {
	ioErr  io.WriteCloser
	ioOut  io.WriteCloser
	ioIn   io.ReadCloser
	getenv func(string) string
	loc    *time.Location
}

// Equivalent to io.NopCloser for writers
type writeCloser struct {
	io.Writer
}

func (writeCloser) Close() error {
	return nil
}

func (t term) Printf(f string, a ...any) (int, error) {
	return fmt.Fprintf(t.ioOut, f, a...)
}

func (t term) Infof(f string, a ...any) (int, error) {
	return fmt.Fprintf(t.ioOut, f, a...)
}

func (t term) EPrintf(f string, a ...any) (int, error) {
	return fmt.Fprintf(t.ioErr, f, a...)
}

func (t term) IOErr() io.Writer {
	return t.ioErr
}

func (t term) IOOut() io.Writer {
	return t.ioOut
}

func (t term) IOIn() io.ReadCloser {
	return t.ioIn
}

func (t term) IsInteractive() bool {
	return isTerminal(t.ioIn)
}

func (t term) IsOutTTY() bool {
	return isTerminal(t.ioOut)
}

func (t term) TimeLocation() *time.Location {
	return t.loc
}

func (t term) Getenv(name string) string {
	return t.getenv(name)
}

func (t term) RunPrompt(p *promptui.Prompt) (string, error) {
	p.Stdout = t.ioOut
	p.Stdin = t.ioIn
	return p.Run()
}

func (t term) OpenBrowser(ctx context.Context, url string) bool {
	return browser.Open(ctx, url)
}
