package cli

import (
	"github.com/cenkalti/backoff/v7"
	"github.com/gemfury/cli/api"
	"github.com/gemfury/cli/internal/ctx"
	"github.com/gemfury/cli/pkg/auth"
	"github.com/gemfury/cli/pkg/terminal"
	"github.com/spf13/cobra"

	"bufio"
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Initialize new Gemfury API client with authentication
func newAPIClient(cc context.Context) (*api.Client, error) {
	token, err := contextAuthToken(cc)
	if err != nil {
		return nil, err
	}

	return newAPIClientWithToken(cc, token)
}

// Initialize new Gemfury API client with an explicit token,
// bypassing the usual resolution by contextAuthToken
func newAPIClientWithToken(cc context.Context, token string) (*api.Client, error) {
	flags := ctx.GlobalFlags(cc)
	c := api.NewClient(token, contextAccount(cc))

	// FURY_RETRIES overrides the default count
	if env := contextEnv(cc, "FURY_RETRIES"); env != "" {
		retries, err := strconv.Atoi(env)
		if err != nil || retries < 0 {
			return nil, usageErrorf("FURY_RETRIES must be a count, not %q", env)
		}
		c.Retries = retries
	}

	// Endpoint overrides (testing, staging), from the fields or the environment
	if u, err := endpointOf(cc, flags.PushEndpoint, "FURY_PUSH_URL"); err != nil {
		return nil, err
	} else if u != nil {
		c.PushEndpoint = u
	}
	if u, err := endpointOf(cc, flags.Endpoint, "FURY_API_URL"); err != nil {
		return nil, err
	} else if u != nil {
		c.Endpoint = u
	}

	// --timeout is that of every request, long ones included; zero is the default
	if d := flags.HTTPTimeout; d < 0 {
		return nil, usageErrorf("--timeout cannot be negative: %s", d)
	} else if d > 0 {
		c.Timeout, c.LongTimeout = d, d
	}

	// A notice of a wait is not a warning, so --quiet hides it; EPrintf alone would not
	if term := ctx.Terminal(cc); term.Mode().ShowInfo {
		c.OnRetry = func(notice string) { term.EPrintf("%s\n", notice) }
	}

	return c, nil
}

// Extract authentication token from context (inline or saved by "login")
func contextAuthToken(cc context.Context) (string, error) {
	if token, err := inlineAuthToken(cc); token != "" || err != nil {
		return token, err
	}
	_, token, err := ctx.Auth(cc).Auth()
	return token, err
}

// inlineAuthToken is the token given for this invocation, rather than
// saved by "login": --api-token or else FURY_TOKEN. With --api-token -,
// the token is read from stdin once, and kept for the rest of the run.
func inlineAuthToken(cc context.Context) (string, error) {
	flags := ctx.GlobalFlags(cc)
	if flags.AuthToken == "-" {
		token, err := readToken(cc)
		if err != nil {
			return "", err
		}
		flags.AuthToken = token
	}
	return flagOrEnv(cc, flags.AuthToken, "FURY_TOKEN"), nil
}

// readToken reads the token for --api-token -: asked for, masked, when
// there is a user to ask, or else the first line of stdin
func readToken(cc context.Context) (string, error) {
	term := ctx.Terminal(cc)
	token, err := term.Ask(cc, terminal.Prompt{Label: "API token", Secret: true})
	if errors.Is(err, terminal.ErrNoInput) {
		in := bufio.NewScanner(term.IOIn())
		in.Scan() // Nothing to read leaves the token empty, which is refused below
		token, err = in.Text(), in.Err()
	}

	if errors.Is(err, bufio.ErrTooLong) {
		return "", usageErrorf("The token given for --api-token - is too long")
	} else if err != nil {
		return "", err
	} else if token = strings.TrimSpace(token); token == "" || token == "-" { // "-" would be read again
		return "", usageErrorf("No token was given for --api-token -")
	}
	return token, nil
}

// contextAccount is the account to act on: --account or else FURY_ACCOUNT.
// When empty, it is the account of the authenticated user.
func contextAccount(cc context.Context) string {
	return flagOrEnv(cc, ctx.GlobalFlags(cc).Account, "FURY_ACCOUNT")
}

// flagOrEnv is the value of a global flag or, when the flag
// is not given, of the environment variable standing in for it
func flagOrEnv(cc context.Context, flag, env string) string {
	if flag != "" {
		return flag
	}
	return contextEnv(cc, env)
}

// contextEnv is the value of an environment variable, trimmed, as read
// from the environment of the terminal rather than of the process
func contextEnv(cc context.Context, name string) string {
	return strings.TrimSpace(ctx.Terminal(cc).Getenv(name))
}

// endpointOf is the base URL of an API: the field a host or a test sets,
// parsed as given, or else the environment variable, which gives a URL or
// names a host (https assumed). The URL may have a path, below which the
// client requests, but a query or a fragment is no endpoint, so it is
// refused. Nil leaves the client's default.
func endpointOf(cc context.Context, field, env string) (*url.URL, error) {
	if field != "" {
		return url.Parse(field)
	}

	value := contextEnv(cc, env)
	if value == "" {
		return nil, nil
	}

	raw := value
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(raw, "?#") {
		return nil, usageErrorf("%s must be a URL or host, not %q", env, value)
	}

	return u, nil
}

// skipAuthKey is the annotation of a command that skips authentication
const skipAuthKey = "fury.skip-auth"

// skipAuth marks a command that must run without authentication,
// such as the ones that manage the session itself
func skipAuth() map[string]string {
	return map[string]string{skipAuthKey: "true"}
}

// skipsAuth reports whether cmd runs without authentication: commands
// annotated with skipAuth, plus Cobra's built-in top-level
// help and shell-completion commands
func skipsAuth(cmd *cobra.Command) bool {
	if cmd.Annotations[skipAuthKey] == "true" {
		return true
	}

	if cmd.Parent() == cmd.Root() {
		switch cmd.Name() {
		case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return true
		}
	}

	return false
}

// Hook for root command to ensure user is authenticated or prompt to login
func preRunCheckAuthentication(cmd *cobra.Command, args []string) error {
	if skipsAuth(cmd) {
		return nil
	}

	_, err := ensureAuthenticated(cmd, false)
	return err
}

// errLoginCancelled is returned when the user backs out of the login
// prompt. "login" exits quietly on it; other commands report it.
var errLoginCancelled = errors.New("Login cancelled")

// ErrNotLoggedIn is returned when there are no credentials and no user at
// the terminal to login (a pipe, CI, an agent), so login is not attempted
var ErrNotLoggedIn = errors.New(`Not logged in. Set FURY_TOKEN or run "fury login" in a terminal.`)

// ErrLoginUnattended is returned by "login" when there is no user to ask
// at the terminal, or none to be asked, whatever the saved credentials
var ErrLoginUnattended = errors.New("Cannot login with no one to ask. Pass --api-token to save a session instead.")

func ensureAuthenticated(cmd *cobra.Command, interactive bool) (*api.AccountResponse, error) {
	cc := cmd.Context()
	var err error

	// Already authenticated with a token given inline or saved by "login"
	if token, err := contextAuthToken(cc); token != "" || err != nil {
		return nil, err
	}

	// Login needs a user to answer its prompts. Without one,
	// fail before any request rather than start a login.
	if !ctx.Terminal(cc).IsInteractive() {
		return nil, ErrNotLoggedIn
	}

	// Trigger browser login
	var resp *api.LoginResponse
	if !interactive {
		resp, err = browserLogin(cmd)
		interactive = errors.Is(err, api.ErrNotImplemented)
	}

	// Trigger interactive login if requested by user
	// or if browser returned "not-implemented"
	if interactive {
		resp, err = interactiveLogin(cmd)
	}

	// Backing out of a prompt cancels the login: with "q" before the browser
	// opens, or with Ctrl-C at an email or password prompt, where it is a
	// key, so the context is live. A signal ends the context, and the
	// command with it, as interrupted.
	switch {
	case errors.Is(err, terminal.ErrAborted),
		errors.Is(err, context.Canceled) && cc.Err() == nil:
		return nil, errLoginCancelled
	case err != nil:
		return nil, err
	}

	// Save credentials for future commands. A token that
	// is saved nowhere is not left valid either.
	if err := saveSession(cc, resp.User.Login(), resp.Token); err != nil {
		if c, err := newAPIClientWithToken(cc, resp.Token); err == nil {
			c.Logout(cc)
		}
		return nil, err
	}

	return &resp.User, nil
}

// saveSession saves the session for the commands that follow, and tells
// the user when it is not in the keychain, where it is expected to be, or
// when Git is not set to ask this CLI for it
func saveSession(cc context.Context, email, token string) error {
	store := ctx.Auth(cc)
	if err := store.Append(email, token); err != nil {
		return err
	}

	if fb, ok := store.(auth.FallbackStore); ok {
		term := ctx.Terminal(cc)
		if path := fb.Fallback(); path != "" {
			term.Infof("Credentials are saved in %s, as the system keychain is not available\n", path)
		}
		if setup := fb.GitSetup(); len(setup) > 0 {
			term.Warnf("Git is not set to ask this CLI for credentials, as its configuration could not be changed. To set it by hand:\n  %s\n", strings.Join(setup, "\n  "))
		}
	}

	return nil
}

// loginPollTimeout is how long browserLogin waits for the user to approve
var loginPollTimeout = 3 * time.Minute

// loginPollInterval is how long browserLogin waits between polls
var loginPollInterval = 500 * time.Millisecond

// browserLogin is a challenge/response authentication via browser
func browserLogin(cmd *cobra.Command) (*api.LoginResponse, error) {
	cc := cmd.Context()
	term := ctx.Terminal(cc)

	c, err := newAPIClient(cc)
	if err != nil {
		return nil, err
	}

	// Generate authentication URLs
	createResp, err := c.LoginCreate(cc)
	if err != nil {
		return nil, err
	} else if createResp.BrowserURL == "" {
		return nil, errors.New("Internal error")
	}

	// Everything is ready. Confirm opening browser to login
	anyKey := "Press any key to login via the browser or q to exit: "
	if err := term.PressAnyKey(anyKey); err != nil {
		return nil, err
	}

	// Attempt to open the browser to create CLI token. The login's lines are
	// on stderr, as its prompts are: any command may start it, with stdout
	// piped to where its result is read.
	term.EPrintf("Opening %s\n", createResp.BrowserURL)
	if ok := term.OpenBrowser(cc, createResp.BrowserURL); !ok {
		if err := cc.Err(); err != nil {
			return nil, err // Interrupted, rather than failed
		}
		term.EPrintf("Failed to open browser. You can continue CLI login by manually opening the URL\n")
	}

	// Start/end spinner while waiting for browser auth
	onDone := term.Spin(" Waiting ...")
	defer onDone()

	// LoginGet API will block & timeout, so we poll until a time limit,
	// which is shorter than the expiry of all the JWT tokens. The deadline
	// also ends a poll that is in flight, unlike backoff's own time limit.
	pollCtx, cancel := context.WithTimeout(cc, loginPollTimeout)
	defer cancel()

	resp, err := backoff.Retry(pollCtx, func() (*api.LoginGetResponse, error) {
		resp, err := c.LoginGet(pollCtx, createResp)
		if !errors.Is(err, api.ErrTimeout) && !errors.Is(err, api.ErrNotFound) {
			err = backoff.Permanent(err) // Retry only on timeout or not-found
		}
		return resp, err
	},
		// We retry with constant backoff waiting for user to approve login
		backoff.WithBackOff(backoff.NewConstantBackOff(loginPollInterval)),
		backoff.WithMaxElapsedTime(0), // Limited by pollCtx
	)

	// Report why the login failed, rather than why polling stopped
	if re := backoff.AsRetryError(err); re != nil {
		switch {
		case cc.Err() != nil:
			err = cc.Err() // Interrupted
		case pollCtx.Err() != nil || errors.Is(re.LastErr, api.ErrNotFound):
			err = api.ErrTimeout
		default:
			err = re.LastErr
		}
	}

	if resp == nil {
		return nil, err
	}

	if resp.Error != "" {
		err = errors.New(resp.Error)
	}

	return &resp.LoginResponse, err
}

// interactiveLogin is an email/password authentication via terminal
func interactiveLogin(cmd *cobra.Command) (*api.LoginResponse, error) {
	cc := cmd.Context()
	term := ctx.Terminal(cc)
	term.EPrintf("Please enter your Gemfury credentials.\n") // On stderr, as browserLogin says

	eResult, err := term.Ask(cc, terminal.Prompt{Label: "Email"})
	if err != nil {
		return nil, err
	}

	pResult, err := term.Ask(cc, terminal.Prompt{Label: "Password", Secret: true})
	if err != nil {
		return nil, err
	}

	c, err := newAPIClient(cc)
	if err != nil {
		return nil, err
	}

	req := api.LoginRequest{Email: eResult, Password: pResult}
	return c.Login(cc, &req)
}
