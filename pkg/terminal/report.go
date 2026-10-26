package terminal

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"fmt"
	"io"
	"strings"
)

// Marks before a line, as GitHub's CLI shows them: what went well, what
// failed, and what to be warned of. They are text, kept when piped; only
// their colours go.
const (
	successMark = "✔"
	failureMark = "✗"
	warningMark = "!"
)

// What leads a line a command reports, as GitHub's CLI colours it, and the
// style of a hint. What of the colours reaches a stream is up to the
// stream (stdout, stderr).
var (
	successLead = lipgloss.NewStyle().Foreground(lipgloss.Green).Render(successMark)
	failureLead = lipgloss.NewStyle().Foreground(lipgloss.Red).Render(failureMark)
	warningLead = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Render(warningMark)
	errorLead   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Red).Render("Error:")
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
)

// Successf prints a result that went well, after a green mark, on stdout.
// It is information, as Infof prints: --quiet leaves it out.
func (u *UI) Successf(f string, a ...any) (int, error) {
	if !u.Mode().ShowInfo {
		return 0, nil
	}
	return report(u.stdout(), successLead, f, a...)
}

// Warnf prints a warning, after a yellow mark, on stderr
func (u *UI) Warnf(f string, a ...any) (int, error) {
	return report(u.stderr(), warningLead, f, a...)
}

// Failf prints the failure of one item, after a red mark, on stderr
func (u *UI) Failf(f string, a ...any) (int, error) {
	return report(u.stderr(), failureLead, f, a...)
}

// Errorf prints the error that ends a command, after "Error:" in bold red,
// on stderr
func (u *UI) Errorf(f string, a ...any) (int, error) {
	return report(u.stderr(), errorLead, f, a...)
}

// report prints a line on w: lead, a space, and the formatted text
func report(w io.Writer, lead, f string, a ...any) (int, error) {
	return fmt.Fprintf(w, "%s %s", lead, fmt.Sprintf(f, a...))
}

// Hintf prints what to do next, muted, on stderr. Each line is styled on
// its own, as a style pads the lines it renders together to one width.
func (u *UI) Hintf(f string, a ...any) (int, error) {
	lines := strings.Split(fmt.Sprintf(f, a...), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = mutedStyle.Render(line)
		}
	}
	return fmt.Fprint(u.stderr(), strings.Join(lines, "\n"))
}

// stdout and stderr are the streams of the session for text with colours,
// which are kept, downsampled or stripped as the stream and the environment
// of the session allow (a pipe gets none; NO_COLOR and CLICOLOR_FORCE are
// honoured), as a program's are. Each write is handled on its own, so a
// styled line goes in one write, as report and Hintf do.
func (u *UI) stdout() io.Writer {
	return &colorprofile.Writer{Forward: u.session.Out, Profile: u.profile(&u.outProfile, u.session.Out)}
}

func (u *UI) stderr() io.Writer {
	return &colorprofile.Writer{Forward: u.session.Err, Profile: u.profile(&u.errProfile, u.session.Err)}
}

// profile is the colour profile of a stream, detected once and kept in
// cache: detection reads the environment, probes the stream, and under
// tmux runs a command, which a line of output should not pay for
func (u *UI) profile(cache **colorprofile.Profile, w io.Writer) colorprofile.Profile {
	if *cache == nil {
		p := colorprofile.Detect(w, u.env.list())
		*cache = &p
	}
	return **cache
}

// forgetProfiles has the profiles detected again, after a knob of a test
// has changed a stream or the environment
func (u *UI) forgetProfiles() {
	u.outProfile, u.errProfile = nil, nil
}
