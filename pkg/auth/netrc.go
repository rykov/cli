package auth

import (
	"github.com/bgentry/go-netrc/netrc"

	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unicode"
)

// Machines for Gemfury in .netrc file
var netrcMachines = []string{"api.fury.io", GitHost}

// Netrc is the session in the .netrc file, whose path is read
// from the environment by getenv
func Netrc(getenv func(string) string) Store {
	return newNetrc(getenv)
}

// newNetrc is Netrc as the concrete nrc, for the credential store
func newNetrc(getenv func(string) string) nrc {
	return nrc{machines: netrcMachines, getenv: getenv}
}

type nrc struct {
	machines []string
	getenv   func(string) string
}

func (n nrc) Auth() (string, string, error) {
	net, _, _, err := n.load()
	if err != nil {
		return "", "", err
	}

	machine := findMachine(net, n.machines[0])
	if machine == nil {
		return "", "", nil
	}

	return machine.Login, machine.Password, nil
}

func (n nrc) Append(user, pass string) error {
	return n.update(func(net *netrc.Netrc) {
		// Entries that are there already would be found before the new ones
		n.remove(net)
		for _, m := range n.machines {
			net.NewMachine(m, user, pass, "")
		}
	})
}

func (n nrc) Wipe() error {
	return n.update(n.remove)
}

// remove removes every entry of each machine, as it may have several
func (n nrc) remove(net *netrc.Netrc) {
	for _, m := range n.machines {
		for findMachine(net, m) != nil {
			net.RemoveMachine(m)
		}
	}
}

// findMachine is the entry of a machine, if it has one. Unlike FindMachine,
// it is never the "default" entry, which holds the credentials of other
// hosts and not of Gemfury.
func findMachine(net *netrc.Netrc, name string) *netrc.Machine {
	if m := net.FindMachine(name); m != nil && !m.IsDefault() {
		return m
	}
	return nil
}

// load parses the .netrc file, and returns its content and its path
// as well. Without a file, it is read as an empty one.
func (n nrc) load() (*netrc.Netrc, []byte, string, error) {
	path, err := n.path()
	if err != nil {
		return nil, nil, "", err
	}

	var net *netrc.Netrc
	data, err := os.ReadFile(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		net, err = parseNetrc(data)
	}

	if err != nil {
		return nil, nil, "", fmt.Errorf("Error reading .netrc file %q: %w", path, err)
	}

	return net, data, path, nil
}

// parseNetrc parses the content of a .netrc file, such that
// NewMachine can add entries to it
func parseNetrc(data []byte) (*netrc.Netrc, error) {
	// NewMachine starts an entry with a line break, which would leave
	// an empty line after the one that ends the file
	data = bytes.TrimRightFunc(data, unicode.IsSpace)

	// Parsed as it is first, for an error to have the line of the file itself
	net, err := netrc.Parse(bytes.NewReader(data))
	if err != nil || !bytes.HasPrefix(data, []byte("default")) {
		return net, err
	}

	// NewMachine adds an entry right before "default", and on the same line
	// when the file starts with it. A line break ahead of it keeps them
	// apart, and is trimmed when the file is written.
	return netrc.Parse(bytes.NewReader(append([]byte("\n"), data...)))
}

// update applies change to the .netrc file, and writes it if it changed
func (n nrc) update(change func(net *netrc.Netrc)) error {
	net, data, path, err := n.load()
	if err != nil {
		return err
	}

	// Apply updates
	before, _ := net.MarshalText()
	change(net)

	// Without a change, there is nothing to write, nor a file to create
	out, _ := net.MarshalText()
	if bytes.Equal(before, out) {
		return nil
	}

	// The file ends with a line break, as a text file does, unless it is empty
	out = bytes.TrimSpace(out)
	if len(out) > 0 {
		out = append(out, '\n')
	}

	// A file with a macro is not written: the library does not keep the
	// empty line that ends one, without which other readers take the
	// entries after it for the macro, or reject the file
	if slices.Contains(strings.Fields(string(data)), "macdef") {
		return fmt.Errorf("Error updating .netrc file %q: its macros (macdef) would not be kept. "+
			"Edit its entries for api.fury.io and git.fury.io by hand, or set FURY_TOKEN to authenticate instead.", path)
	}

	return replaceFile(path, out)
}

// Variables for tests to stub these two steps
var (
	createTemp = os.CreateTemp
	renameFile = os.Rename
)

// replaceFile writes a temporary file next to path, and renames it over
// path, so that the file is never left half-written. It is readable by its
// owner only, even where it existed with other permissions. Its owner, and
// its hard links, are not kept: it is a new file.
//
// Where the file cannot be replaced, it is written in place, which an
// interruption may cut short.
func replaceFile(path string, data []byte) error {
	// Write through a link, as from a dotfiles directory, to keep it
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}

	// CreateTemp makes the file 0600. Fall back to writing in place only
	// when the directory takes no new file: no permission, or a read-only
	// filesystem. After any other failure, such as a full disk, writing in
	// place would only truncate the real file, so the error is returned.
	tmp, err := createTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return writeInPlace(path, data)
	} else if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // Has nothing to remove once renamed

	// Synced, or a crash may leave the renamed file empty
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
		return err
	}

	// A file that is mounted alone, as in a container, is not renamed over
	if err := renameFile(tmp.Name(), path); err != nil {
		return writeInPlace(path, data)
	}

	return nil
}

// writeInPlace overwrites the file. WriteFile keeps the permissions of a
// file that exists, so they are set before it has the credentials.
func writeInPlace(path string, data []byte) error {
	if err := os.Chmod(path, 0600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// path is the path of the .netrc file: that of NETRC in the environment,
// or else the one in the home directory of the process
func (n nrc) path() (string, error) {
	if path := n.getenv("NETRC"); path != "" {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(home, "_netrc"), nil
	}

	return filepath.Join(home, ".netrc"), nil
}
