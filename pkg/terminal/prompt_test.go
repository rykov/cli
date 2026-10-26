package terminal

import (
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A question is shown as one bold line, as promptui showed it: the label,
// then the answer being typed, after the default of a yes/no question
func TestPromptShown(t *testing.T) {
	var answer string

	for name, tc := range map[string]struct {
		field huh.Field
		shown string // The question
		then  string // What follows it on the line
	}{
		"prompt":        {Prompt{Label: "Email"}.field(&answer), "? Email:", ""},
		"secret prompt": {Prompt{Label: "API token", Secret: true}.field(&answer), "? API token:", ""},
		"confirm":       {confirmField("Are you sure you want to logout?", false, &answer), "? Are you sure you want to logout? (y/N)", ""},
		"confirm, yes":  {confirmField("Continue?", true, &answer), "? Continue? (Y/n)", ""},
	} {
		t.Run(name, func(t *testing.T) {
			form := promptForm(tc.field).WithWidth(80)
			form.Init()
			shown := ansi.Strip(form.View())
			if !strings.HasPrefix(shown, tc.shown) {
				t.Errorf("Expected %q to be shown, got %q", tc.shown, shown)
			}
			if rest := strings.TrimSpace(shown[min(len(tc.shown), len(shown)):]); rest != tc.then {
				t.Errorf("Expected %q after the question, got %q", tc.then, rest)
			}
		})
	}
}

// Once answered, a question is left behind with its answer, masked for a
// secret, so that the transcript shows what was asked
func TestPromptAnswered(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		want string
	}{
		"prompt":  {Prompt{Label: "Email"}.answered("u@example.com"), "✔ Email: u@example.com"},
		"secret":  {Prompt{Label: "API token", Secret: true}.answered("héllo"), "✔ API token: *****"},
		"yes":     {confirmAnswered("Sure?", true), "✔ Sure? Yes"},
		"no":      {confirmAnswered("Sure?", false), "✗ Sure? No"},
		"nothing": {Prompt{Label: "Email"}.answered(""), "✔ Email: "},
	} {
		t.Run(name, func(t *testing.T) {
			if line := ansi.Strip(tc.line); line != tc.want {
				t.Errorf("Expected %q, got %q", tc.want, line)
			}
		})
	}
}

// A question and its answer are coloured as GitHub's CLI colours them: the
// mark green, or red for a no, the question bold while asked, the answer
// cyan. The colours reach a terminal, or a stream that asks for them with
// CLICOLOR_FORCE, and no other.
func TestPromptStyled(t *testing.T) {
	const bold, red, green, cyan = "1", "31", "32", "36" // SGR parameters

	if q := question("Sure?"); !sgr(q, green) || !sgr(q, bold) || ansi.Strip(q) != "? Sure?" {
		t.Errorf("Expected a bold green mark before a bold question, got %q", q)
	}
	if a := confirmAnswered("Sure?", true); !sgr(a, green) || !sgr(a, cyan) || sgr(a, bold) {
		t.Errorf("Expected a green mark and a cyan answer, in plain weight, got %q", a)
	}
	if a := confirmAnswered("Sure?", false); !sgr(a, red) || !sgr(a, cyan) {
		t.Errorf("Expected a red mark and a cyan answer, got %q", a)
	}

	for name, tc := range map[string]struct {
		env     map[string]string
		colours bool
	}{
		"pipe":   {nil, false},
		"forced": {forcedColour, true},
	} {
		t.Run(name, func(t *testing.T) {
			term, _, out := askedUI("y\r", tc.env)
			if _, err := term.Confirm(t.Context(), "Sure?", false); err != nil {
				t.Fatal(err)
			}
			shown := out.String()
			if coloured := sgr(shown, green) || sgr(shown, cyan); coloured != tc.colours {
				t.Errorf("Expected colours=%v, got %q", tc.colours, shown)
			}
			if !strings.HasSuffix(ansi.Strip(shown), "✔ Sure? Yes\n") {
				t.Errorf("Expected the answered line, got %q", shown)
			}
		})
	}
}

// forcedColour is an environment that asks a stream for colours, whether
// or not a person is at it
var forcedColour = map[string]string{"CLICOLOR_FORCE": "1", "TERM": "xterm-256color"}

// sgrRegexp matches a styling sequence, with its parameters
var sgrRegexp = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// sgr reports whether s is styled with the given SGR parameter, such as
// "1" for bold or "32" for green, alone or among others
func sgr(s, param string) bool {
	for _, m := range sgrRegexp.FindAllStringSubmatch(s, -1) {
		if slices.Contains(strings.Split(m[1], ";"), param) {
			return true
		}
	}
	return false
}

// A question is asked at a real form, which reads keys until it is answered,
// and is left behind as one line with the answer; Ctrl-C, Ctrl-D and the end
// of the command's context interrupt it, and leave no such line. The keys
// are read from a buffer, whose end the program takes in its stride.
//
// With ACCESSIBLE set in the session, as huh suggests, the question is
// plain lines for a screen reader, with its hint, and nothing moves the
// cursor or redraws. A secret is asked at the form, as plain lines would
// need a terminal to hide it. The end of ctx, which the line read does not
// notice, is noticed after it.
func TestPromptAsked(t *testing.T) {
	accessible := map[string]string{"ACCESSIBLE": "1"}
	for name, tc := range map[string]struct {
		keys    string
		env     map[string]string
		confirm bool // Confirm, rather than Ask
		dflt    bool // Of Confirm
		secret  bool
		cancel  bool   // The command's context is over before the question
		answer  string // Of Ask
		yes     bool   // Of Confirm
		err     error
		asked   string // How the question is shown in plain lines; empty for a redrawn form
		shown   string // What the output ends with, without its styling
	}{
		"prompt":            {keys: "u@example.com\r", answer: "u@example.com", shown: "✔ Email: u@example.com\n"},
		"secret":            {keys: "s3cret\r", secret: true, answer: "s3cret", shown: "✔ Email: ******\n"},
		"yes":               {keys: "y\r", confirm: true, yes: true, shown: "✔ Sure? Yes\n"},
		"no":                {keys: "n\r", confirm: true, shown: "✗ Sure? No\n"},
		"yes, spelt out":    {keys: "yes\r", confirm: true, yes: true, shown: "✔ Sure? Yes\n"},
		"no, in capitals":   {keys: "NO\r", confirm: true, shown: "✗ Sure? No\n"},
		"default no":        {keys: "\r", confirm: true, shown: "✗ Sure? No\n"},
		"default yes":       {keys: "\r", confirm: true, dflt: true, yes: true, shown: "✔ Sure? Yes\n"},
		"refused, then yes": {keys: "x\r\x7fy\r", confirm: true, yes: true, shown: "✔ Sure? Yes\n"}, // Enter on "x" is refused; backspace, then y
		"ctrl-c":            {keys: "\x03", err: context.Canceled},
		"ctrl-c at confirm": {keys: "\x03", confirm: true, err: context.Canceled},
		"ctrl-d":            {keys: "\x04", err: context.Canceled},
		"ctrl-d at confirm": {keys: "\x04", confirm: true, err: context.Canceled},
		"context over":      {cancel: true, err: context.Canceled},

		"plain confirm":      {keys: "y\n", env: accessible, confirm: true, yes: true, asked: "? Sure? (y/N) ", shown: "✔ Sure? Yes\n"},
		"plain prompt":       {keys: "u@example.com\n", env: accessible, answer: "u@example.com", asked: "? Email: ", shown: "✔ Email: u@example.com\n"},
		"plain secret":       {keys: "s3cret\r", env: accessible, secret: true, answer: "s3cret", shown: "✔ Email: ******\n"},
		"plain context over": {keys: "y\n", env: accessible, confirm: true, cancel: true, err: context.Canceled, asked: "? Sure? (y/N) "},
	} {
		t.Run(name, func(t *testing.T) {
			term, stdout, out := askedUI(tc.keys, tc.env)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}

			var answer string
			var yes bool
			var err error
			inTime(t, func() {
				if tc.confirm {
					yes, err = term.Confirm(ctx, "Sure?", tc.dflt)
				} else {
					answer, err = term.Ask(ctx, Prompt{Label: "Email", Secret: tc.secret})
				}
			})
			if !errors.Is(err, tc.err) || answer != tc.answer || yes != tc.yes {
				t.Errorf("Expected %q, %v and error %v, got %q, %v and: %v", tc.answer, tc.yes, tc.err, answer, yes, err)
			}

			if stdout.Len() != 0 {
				t.Errorf("Expected nothing on stdout, got %q", stdout.String())
			}
			if plain := tc.asked != ""; redrawRegexp.MatchString(out.String()) == plain {
				t.Errorf("Expected plain lines=%v, got %q", plain, out.String())
			}
			shown := ansi.Strip(out.String())
			if !strings.HasPrefix(shown, tc.asked) {
				t.Errorf("Expected output to start with %q, got %q", tc.asked, shown)
			}
			if !strings.HasSuffix(shown, tc.shown) {
				t.Errorf("Expected output to end with %q, got %q", tc.shown, shown)
			} else if tc.shown == "" && (strings.Contains(shown, successMark) || strings.Contains(shown, failureMark)) {
				t.Errorf("Expected no answer left behind, got %q", shown)
			}
		})
	}
}

// redrawRegexp matches a private sequence, which queries or sets a mode of
// the terminal: what a form that is redrawn emits, from its first query on,
// and plain lines never do
var redrawRegexp = regexp.MustCompile(`\x1b\[\?`)

// A typed answer to a yes/no question is y, yes, n or no in any case, or
// nothing for the default; anything else is no answer
func TestYesOrNo(t *testing.T) {
	for answer, want := range map[string]struct{ yes, ok bool }{
		"":      {false, true}, // The default, false here
		" ":     {false, true},
		"y":     {true, true},
		"Y":     {true, true},
		"yes":   {true, true},
		"YES":   {true, true},
		" yes ": {true, true},
		"n":     {false, true},
		"N":     {false, true},
		"no":    {false, true},
		"No":    {false, true},
		"maybe": {false, false},
		"yess":  {false, false},
	} {
		if yes, ok := yesOrNo(answer, false); yes != want.yes || ok != want.ok {
			t.Errorf("yesOrNo(%q, false): expected %v, %v, got %v, %v", answer, want.yes, want.ok, yes, ok)
		}
	}
	if yes, ok := yesOrNo("", true); !yes || !ok {
		t.Errorf("yesOrNo(\"\", true): expected the default, got %v, %v", yes, ok)
	}
}

// One key answers PressAnyKey, which echoes a newline for it; "q", Esc,
// Ctrl-C and Ctrl-D back out, as does a closed stdin, before the newline
func TestPressAnyKey(t *testing.T) {
	const prompt = "Press any key: "
	for name, tc := range map[string]struct {
		key   string
		err   error
		shown string
	}{
		"any key": {"!", nil, prompt + "\n"},
		"q":       {"q", ErrAborted, prompt + "\n"},
		"Q":       {"Q", ErrAborted, prompt + "\n"},
		"esc":     {"\x1b", ErrAborted, prompt + "\n"},
		"ctrl-c":  {"\x03", ErrAborted, prompt + "\n"},
		"ctrl-d":  {"\x04", ErrAborted, prompt + "\n"},
		"closed":  {"", ErrAborted, prompt},
	} {
		t.Run(name, func(t *testing.T) {
			term, _, out := askedUI(tc.key, nil)
			if err := term.PressAnyKey(prompt); !errors.Is(err, tc.err) {
				t.Errorf("Expected error %v, got: %v", tc.err, err)
			}
			if out.String() != tc.shown {
				t.Errorf("Expected %q shown, got %q", tc.shown, out.String())
			}
		})
	}
}

// askedUI is a UI with a person at stdin and at stderr, who types keys, in
// the given environment. Questions are on stderr; stdout is the result alone.
func askedUI(keys string, env map[string]string) (term *UI, stdout, stderr *bytes.Buffer) {
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	term = NewSession(Session{In: strings.NewReader(keys), Out: stdout, Err: stderr, Env: env, Caps: Caps{InTTY: true, ErrTTY: true}})
	return term, stdout, stderr
}

// A question needs a terminal to be asked on. A pipe or a buffer has no one
// to answer, so nothing is shown and ErrNoInput is returned.
func TestPromptNonTerminal(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()

	var out bytes.Buffer
	for name, in := range map[string]io.Reader{
		"pipe":   pr,
		"buffer": strings.NewReader("y\n"),
	} {
		t.Run(name, func(t *testing.T) {
			// The stream is probed, as New does for those of the process
			term := NewSession(Session{In: in, Err: &out, Caps: Caps{InTTY: isTerminal(in), ErrTTY: true}})
			if _, err := term.Ask(t.Context(), Prompt{Label: "Email"}); !errors.Is(err, ErrNoInput) {
				t.Errorf("Expected ErrNoInput from a prompt, got: %v", err)
			}
			if ok, err := term.Confirm(t.Context(), "Sure?", false); ok || !errors.Is(err, ErrNoInput) {
				t.Errorf("Expected ErrNoInput from a confirm, got %v and: %v", ok, err)
			}
			if err := term.PressAnyKey("Press any key: "); !errors.Is(err, ErrNoInput) {
				t.Errorf("Expected ErrNoInput from a key prompt, got: %v", err)
			}
			if out.Len() != 0 {
				t.Errorf("Expected nothing shown, got %q", out.Bytes())
			}
		})
	}
}
