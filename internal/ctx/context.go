package ctx

import (
	"github.com/gemfury/cli/pkg/terminal"

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
// into, its auther, and its terminal, which has its environment.
//
// It is stored by value, so that a context derived with another terminal
// leaves its parent as it was. The flags are a pointer, shared by both,
// as they are parsed and set after the context is made.
type Cmd struct {
	Flags    *CmdGlobalFlags
	Auther   terminal.Auther
	Terminal terminal.Terminal
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

func Auther(ctx context.Context) terminal.Auther {
	return FromContext(ctx).Auther
}

func Terminal(ctx context.Context) terminal.Terminal {
	return FromContext(ctx).Terminal
}

func TestTerm(ctx context.Context) terminal.TestTerm {
	return Terminal(ctx).(terminal.TestTerm)
}
