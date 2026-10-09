package cli

import (
	"github.com/gemfury/cli/internal/ctx"
	"github.com/gemfury/cli/pkg/terminal"

	"context"
)

// CommandContext is the context for executing commands
// including global flags, auther, and terminal values
func CommandContext() context.Context {
	term := terminal.New()
	return ctx.NewContext(context.Background(), ctx.Cmd{
		Auther:   terminal.CredentialStore(term),
		Terminal: term,
	})
}

// TestContext is the context for executing commands in testing,
// derived from the parent context, such as the one of the test
func TestContext(parent context.Context, t terminal.Terminal, a terminal.Auther) context.Context {
	return ctx.NewContext(parent, ctx.Cmd{Auther: a, Terminal: t})
}
