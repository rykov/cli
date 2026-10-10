package cli_test

import (
	"github.com/gemfury/cli/cli"
	"github.com/gemfury/cli/internal/ctx"
	"github.com/gemfury/cli/internal/testutil"
	"github.com/gemfury/cli/pkg/terminal"
	"github.com/spf13/afero"

	"bytes"
	"net/http"
	"testing"
)

// A host runs a command for a session with the terminal, the auther, and
// the file system of that session, and nothing of its own process
func TestHostSession(t *testing.T) {
	t.Setenv("FURY_TOKEN", "process-token")

	var token, account string
	server := testutil.APIServerCustom(t, func(mux *http.ServeMux) {
		mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
			token, account = r.Header.Get("Authorization"), r.URL.Query().Get("as")
			w.Write([]byte(pushResponse))
		})
	})

	var out, errOut bytes.Buffer
	term := terminal.NewSession(terminal.Session{
		Out: &out,
		Err: &errOut,
		Env: map[string]string{"FURY_ACCOUNT": "session-org"},
	})

	fsys := afero.NewMemMapFs()
	if err := afero.WriteFile(fsys, "/pkg-1.0.0.gem", []byte("gem"), 0600); err != nil {
		t.Fatal(err)
	}

	cc := cli.NewContext(t.Context(), term, terminal.TestAuther("user", "host-token", nil), fsys)
	flags := ctx.GlobalFlags(cc)
	flags.Endpoint, flags.PushEndpoint = server.URL, server.URL

	cmd := cli.NewRootCommand(cc)
	cmd.SetArgs([]string{"push", "/pkg-1.0.0.gem"})
	if err := cli.Execute(cc, cmd); err != nil {
		t.Fatalf("Command error: %v, with %q", err, errOut.String())
	}

	if token != "host-token" || account != "session-org" {
		t.Errorf("Expected the token of the auther and the account of the session, got %q and %q", token, account)
	}
	if exp := "Uploading pkg-1.0.0.gem - done\n"; out.String() != exp || errOut.Len() != 0 {
		t.Errorf("Expected %q on the output of the session and nothing else, got %q and %q", exp, out.String(), errOut.String())
	}
}
