package ctx_test

import (
	"github.com/gemfury/cli/internal/ctx"
	"github.com/gemfury/cli/pkg/auth"
	"github.com/gemfury/cli/pkg/terminal"

	"testing"
)

// A context with another terminal leaves its parent with the one it had,
// and shares its flags, which are set once the context is made
func TestWithTerminal(t *testing.T) {
	term, other := terminal.NewForTest(), terminal.NewForTest()
	parent := ctx.NewContext(t.Context(), ctx.Cmd{
		Auth:     auth.NewMemory("user", "token", nil),
		Terminal: term,
	})
	derived := ctx.WithTerminal(parent, other)

	if ctx.Terminal(parent) != term {
		t.Error("Expected the parent to keep its terminal")
	}
	if ctx.Terminal(derived) != other {
		t.Error("Expected the derived context to have the other terminal")
	}
	if ctx.Auth(derived) != ctx.Auth(parent) {
		t.Error("Expected the derived context to keep the store")
	}

	flags := ctx.GlobalFlags(parent)
	if flags == nil || ctx.GlobalFlags(derived) != flags {
		t.Fatal("Expected both contexts to share the flags")
	}
	flags.Account = "org"
	if got := ctx.GlobalFlags(derived).Account; got != "org" {
		t.Errorf("Expected the flag set on the parent, got %q", got)
	}
}
