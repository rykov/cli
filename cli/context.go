package cli

import (
	"github.com/gemfury/cli/internal/ctx"
	"github.com/gemfury/cli/pkg/auth"
	"github.com/gemfury/cli/pkg/terminal"
	"github.com/spf13/afero"

	"context"
)

// NewContext is the context for executing a command with the given
// terminal, store, and file system, such as those of a session of a host.
// It is for one run of one command, which parses its flags into it.
func NewContext(parent context.Context, t *terminal.UI, a auth.Store, fsys afero.Fs) context.Context {
	return ctx.NewContext(parent, ctx.Cmd{Auth: a, Terminal: t, FS: fsys})
}

// CommandContext is the context for executing commands in the process:
// its terminal, its saved credentials, and its disk
func CommandContext() context.Context {
	term := terminal.New()
	return NewContext(context.Background(), term, auth.New(term), afero.NewOsFs())
}

// TestContext is the context for executing commands in testing,
// derived from the parent context, such as the one of the test
func TestContext(parent context.Context, t *terminal.UI, a auth.Store) context.Context {
	return NewContext(parent, t, a, afero.NewOsFs())
}
