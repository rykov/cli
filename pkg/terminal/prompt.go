package terminal

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	xterm "github.com/charmbracelet/x/term"

	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Prompt is a question with a one-line answer
type Prompt struct {
	Label  string // Shown before the answer, as "Label: "
	Secret bool   // The answer is masked, while typed and once given
}

// ErrAborted is returned when the user backs out of PressAnyKey
var ErrAborted = errors.New("aborted")

// ErrNoInput is returned for a question that there is no one to answer
var ErrNoInput = errors.New("Confirmation needed. Pass --yes to confirm, as there is no one to ask.")

// Ask shows the question on stderr and reads the answer from stdin. Backing
// out with Ctrl-C, or the end of ctx, gives context.Canceled; with no one
// to ask, as the Mode decides, ErrNoInput.
func (u *UI) Ask(ctx context.Context, p Prompt) (string, error) {
	if !u.Mode().Interactive {
		return "", ErrNoInput
	}
	return u.asker.ask(ctx, p)
}

// field is the question as a form field, answered into answer
func (p Prompt) field(answer *string) huh.Field {
	in := inputField(p.Label, ":", answer)
	if p.Secret {
		in.EchoMode(huh.EchoModePassword)
	}
	return in
}

// inputField is a question as a form field, answered into answer: the
// label as question shows it, then the hint, as the title, which is all
// that accessible mode shows; then a space, as the prompt of the answer
func inputField(label, hint string, answer *string) *huh.Input {
	return huh.NewInput().Title(question(label) + hint).Prompt(" ").Inline(true).Value(answer)
}

// answered is the line left behind once the question is answered
func (p Prompt) answered(answer string) string {
	if p.Secret {
		answer = strings.Repeat("*", utf8.RuneCountInString(answer))
	}
	return answeredLine(true, p.Label+":", answer)
}

// answeredLine is a question as left behind once answered: a mark, green
// for yes or red for no, the label, and the answer in cyan
func answeredLine(yes bool, label, answer string) string {
	lead := successLead
	if !yes {
		lead = failureLead
	}
	return lead + " " + label + " " + answerStyle.Render(answer)
}

// Styles of a question, as GitHub's CLI shows one: bold while it is asked,
// after a bold green "?", and plain once answered, with the answer in cyan
// after a mark of report.go
var (
	askingStyle = lipgloss.NewStyle().Bold(true)
	answerStyle = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	askLead     = askingStyle.Foreground(lipgloss.Green).Render("?")
)

// question is the label as shown while it is asked
func question(label string) string {
	return askLead + " " + askingStyle.Render(label)
}

// Confirm asks a yes/no question, answered with Enter alone by dflt, which
// the question shows in capitals. It is answered yes by --yes, and
// otherwise as Ask is asked.
func (u *UI) Confirm(ctx context.Context, label string, dflt bool) (bool, error) {
	switch mode := u.Mode(); {
	case mode.AutoYes:
		return true, nil
	case !mode.Interactive:
		return false, ErrNoInput
	}
	return u.asker.confirm(ctx, label, dflt)
}

// asker asks the questions that the Mode of a UI allows: with forms on the
// streams of the session, or from a script in tests
type asker interface {
	ask(ctx context.Context, p Prompt) (string, error)
	confirm(ctx context.Context, label string, dflt bool) (bool, error)
}

// forms is the asker of a UI: each question is a huh form on stderr, left
// behind as one line with its answer
type forms struct {
	u *UI
}

func (f forms) ask(ctx context.Context, p Prompt) (string, error) {
	var answer string
	if err := f.run(ctx, p.field(&answer), p.Secret); err != nil {
		return "", err
	}
	f.leave(p.answered(answer))
	return answer, nil
}

func (f forms) confirm(ctx context.Context, label string, dflt bool) (bool, error) {
	var answer string
	if err := f.run(ctx, confirmField(label, dflt, &answer), false); err != nil {
		return false, err
	}
	yes, _ := yesOrNo(answer, dflt) // Valid, or the form would still be asking
	f.leave(confirmAnswered(label, yes))
	return yes, nil
}

// confirmField is a yes/no question as a form field, answered into answer
// as typed; one that is neither yes nor no is refused with a hint
func confirmField(label string, dflt bool, answer *string) huh.Field {
	hint := " (y/N)"
	if dflt {
		hint = " (Y/n)"
	}
	return inputField(label, hint, answer).
		Validate(func(s string) error {
			if _, ok := yesOrNo(s, dflt); !ok {
				return errors.New("Please answer y or n")
			}
			return nil
		})
}

// yesOrNo reads a typed answer to a yes/no question: y, yes, n or no in any
// case, or nothing for the default. Anything else is not an answer.
func yesOrNo(answer string, dflt bool) (yes, ok bool) {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return dflt, true
	case "y", "yes":
		return true, true
	case "n", "no":
		return false, true
	}
	return false, false
}

// confirmAnswered is the line left behind once a yes/no question is
// answered: marked as declined when it is
func confirmAnswered(label string, yes bool) string {
	answer := "No"
	if yes {
		answer = "Yes"
	}
	return answeredLine(yes, label, answer)
}

// leave prints the line left behind once a question is answered
func (f forms) leave(line string) {
	fmt.Fprintf(f.u.stderr(), "%s\n", line)
}

// run shows a one-field form on stderr until it is answered. Ctrl-C or
// Ctrl-D, which are keys while the form is shown, and the end of ctx
// interrupt the command rather than answer. The form clears itself once
// answered.
//
// With ACCESSIBLE set in the session, as huh suggests, the form is plain
// lines for a screen reader, never redrawn, which read a line that the end
// of ctx does not interrupt, so that is checked once it is read. A secret
// is asked so of a terminal alone, which huh needs to hide it, and without
// which it would give up silently.
func (f forms) run(ctx context.Context, field huh.Field, secret bool) error {
	accessible := f.u.Getenv("ACCESSIBLE") != "" && (!secret || isTerminal(f.u.session.In))

	// WithProgramOptions replaces the options, so it goes before the streams
	form := promptForm(field).WithProgramOptions(f.u.programOptions()...).
		WithInput(f.u.session.In).WithOutput(f.u.session.Err).
		WithAccessible(accessible)
	err := form.RunWithContext(ctx)
	switch {
	case errors.Is(err, huh.ErrUserAborted), ctx.Err() != nil:
		return context.Canceled
	case errors.Is(err, huh.ErrTimeout):
		// huh reports a killed program as a timeout: the end of ctx, which
		// is handled above, or a failure of the program
		return errors.New("The question could not be shown")
	}
	return err
}

// promptForm is a form of one field, shown as one line
func promptForm(field huh.Field) *huh.Form {
	return huh.NewForm(huh.NewGroup(field)).WithTheme(promptTheme).WithKeyMap(promptKeys).WithShowHelp(false)
}

// promptKeys are huh's keys, with Ctrl-D backing out as Ctrl-C does, as the
// end of input did at promptui
var promptKeys = func() *huh.KeyMap {
	k := huh.NewDefaultKeyMap()
	k.Quit.SetKeys("ctrl+c", "ctrl+d")
	return k
}()

// promptTheme shows a field as one line, as promptui did, rather than in
// huh's bordered block. The title comes styled, by question, and the hint
// after it is plain.
var promptTheme = huh.ThemeFunc(func(isDark bool) *huh.Styles {
	s := huh.ThemeBase(isDark)
	s.Focused.Base = lipgloss.NewStyle()
	s.Focused.Title = lipgloss.NewStyle()
	s.Focused.TextInput.Prompt = lipgloss.NewStyle()
	s.Blurred = s.Focused
	return s
})

// Control bytes that mean "quit" at a single-key prompt. The terminal is in
// raw mode while reading, so Ctrl-C arrives as a byte rather than a signal.
const (
	keyCtrlC = 0x03
	keyCtrlD = 0x04
	keyEsc   = 0x1b
)

// PressAnyKey shows the prompt and reads a single key from stdin. It
// returns ErrAborted when the user backs out with "q", Esc, Ctrl-C, or
// Ctrl-D, and ErrNoInput when there is no one to ask, as the Mode decides.
func (u *UI) PressAnyKey(prompt string) error {
	if !u.Mode().Interactive {
		return ErrNoInput
	}
	ch, err := stdinRawCharPrompt(u, prompt)
	if err != nil {
		return err
	}

	switch ch {
	case 'q', 'Q', keyCtrlC, keyCtrlD, keyEsc:
		return ErrAborted
	}

	return nil
}

// stdinRawCharPrompt shows the prompt on stderr and reads one byte from stdin
func stdinRawCharPrompt(u *UI, prompt string) (byte, error) {
	stdin := u.IOIn()
	u.EPrintf("%s", prompt)

	// A closed stdin cannot answer, which is the same as declining
	var b [1]byte
	if n, err := readRaw(stdin, b[:]); errors.Is(err, io.EOF) {
		return 0, ErrAborted
	} else if err != nil {
		return 0, err
	} else if n == 0 {
		return 0, io.ErrNoProgress
	}

	// A newline for the key, which raw mode did not echo
	u.EPrintf("\n")
	return b[0], nil
}

// readRaw reads from stdin in raw mode, so that a single key is read without
// waiting for Enter. Only a terminal can do this; a pipe or file is read
// as-is. The terminal is back to its previous mode before any output.
func readRaw(stdin io.Reader, b []byte) (int, error) {
	if fd, ok := terminalFd(stdin); ok {
		state, err := xterm.MakeRaw(fd)
		if err != nil {
			return 0, err
		}
		defer xterm.Restore(fd, state)
	}

	return stdin.Read(b)
}
