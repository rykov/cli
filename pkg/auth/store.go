// Package auth keeps the session of a login: the user and the token that
// "fury login" saves, for every command to send. New is the store of the
// process, in the system keychain or in .netrc; Netrc is .netrc alone;
// Memory is for a host, or a test, to hold one in memory.
package auth

// Store is where the session is kept: the user and the token as saved by
// Append, read back by Auth, and removed by Wipe. Auth gives empty strings
// and no error when there is no session.
type Store interface {
	Auth() (user, token string, err error)
	Append(user, token string) error
	Wipe() error
}

// FallbackStore is a Store that tells what its user is to know of the
// session that it has saved last, when that did not go as it would by
// default: it is to be asked right after the session is saved
type FallbackStore interface {
	Store

	// Fallback is the path of .netrc when the session was saved there for
	// want of a keychain, rather than as asked
	Fallback() string

	// GitSetup is the commands that set this CLI as the credential helper
	// of Git, when the session is in the keychain and Git could not be set
	GitSetup() []string
}

// Terminal is what the store needs of the terminal of a session: its
// environment, whether a person is there to unlock a keychain, and where
// to say that it is waiting for one
type Terminal interface {
	Getenv(name string) string
	IsInteractive() bool
	EPrintf(format string, a ...any) (int, error)
}

// Memory is a Store held in memory: that of a host that keeps sessions
// its own way, or of a test. Err, if set, is returned by every method,
// and the session is left as it is.
type Memory struct {
	User  string
	Token string
	Err   error
}

// NewMemory is a Memory that holds the given session, or that fails every
// call with err
func NewMemory(user, token string, err error) *Memory {
	return &Memory{User: user, Token: token, Err: err}
}

func (m *Memory) Auth() (string, string, error) {
	return m.User, m.Token, m.Err
}

func (m *Memory) Append(user, token string) error {
	if m.Err == nil {
		m.User, m.Token = user, token
	}
	return m.Err
}

func (m *Memory) Wipe() error {
	return m.Append("", "")
}
