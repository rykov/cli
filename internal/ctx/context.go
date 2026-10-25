package ctx

import (
	"github.com/gemfury/cli/pkg/auth"
	"github.com/gemfury/cli/pkg/terminal"
	"github.com/spf13/afero"

	"context"
	"time"
)

// cmdKey is the key of the Cmd of a context
type cmdKey struct{}

// CmdGlobalFlags are the global flags as given on the command line. To get
// AuthToken or Account, use the cli helpers that fall back to the
// environment of the terminal, unless only the flag is of interest.
type CmdGlobalFlags struct {
	PushEndpoint string
	Endpoint     string
	AuthToken    string
	Account      string
	Yes          bool
	NoInput      bool
	Quiet        bool
	NoProgress   bool
	HTTPTimeout  time.Duration
}

// Cmd is what a command runs with: its global flags, which Cobra parses
// into, its store of the saved session, its terminal, which has its
// environment, and its file system: the disk of the process, or a root of
// its own for each session of a host.
//
// It is stored by value, so that a context derived with another terminal
// leaves its parent as it was. The flags are a pointer, shared by both,
// as they are parsed and set after the context is made.
type Cmd struct {
	Flags    *CmdGlobalFlags
	Auth     auth.Store
	Terminal terminal.Terminal
	FS       afero.Fs
}

// NewContext is the context of a command. Its flags are made here,
// empty, for Cobra to parse into, unless the caller has made them.
func NewContext(parent context.Context, c Cmd) context.Context {
	if c.Flags == nil {
		c.Flags = &CmdGlobalFlags{}
	}
	return context.WithValue(parent, cmdKey{}, c)
}

// FromContext is what the command of a context runs with
func FromContext(ctx context.Context) Cmd {
	return ctx.Value(cmdKey{}).(Cmd)
}

// WithTerminal replaces the terminal of a command context
func WithTerminal(ctx context.Context, t terminal.Terminal) context.Context {
	c := FromContext(ctx)
	c.Terminal = t
	return NewContext(ctx, c)
}

func GlobalFlags(ctx context.Context) *CmdGlobalFlags {
	return FromContext(ctx).Flags
}

func Auth(ctx context.Context) auth.Store {
	return FromContext(ctx).Auth
}

func Terminal(ctx context.Context) terminal.Terminal {
	return FromContext(ctx).Terminal
}

func FS(ctx context.Context) afero.Fs {
	return FromContext(ctx).FS
}

func TestTerm(ctx context.Context) terminal.TestTerm {
	return Terminal(ctx).(terminal.TestTerm)
}
