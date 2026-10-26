package terminal

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"fmt"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

// The progress bar and the spinner are each drawn by a Bubble Tea program,
// but look exactly as the legacy Gemfury CLI drew them with cheggaaa/pb and
// briandowns/spinner:
//
//	Uploading x [=========⠁         ] 50.00%
//	⣾ Waiting ...
//
// The rules of pb's "bar" and "percent" elements are copied below, so the
// output is unchanged. The cells are drawn here, rather than by the bubbles
// progress component, whose colours, percentage and rounding would each be
// undone to look as pb did. While a bar is shown, the terminal is also told
// its progress, for a tab or a taskbar, where it can show one.

const (
	refreshRate  = 200 * time.Millisecond // pb's redraw rate
	defaultWidth = 100                    // pb's width when the terminal's is unknown
)

// tips are the frames of pb's "cycle" element, the tip of the bar
var tips = []rune("⠁⠂⠄⠂")

// spinSet is briandowns/spinner's character set 11 at its rate
var spinSet = spinner.Spinner{
	Frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
	FPS:    100 * time.Millisecond,
}

// tick asks for the next frame after refreshRate. tea.Tick starts its timer
// at once, so each frame needs a tick of its own.
func tick() tea.Cmd {
	return tea.Tick(refreshRate, func(time.Time) tea.Msg { return tickMsg{} })
}

// StartProgress renders a progress bar on the error stream, when the Mode
// shows progress. Otherwise no bar is shown, since its redraw sequences
// would only corrupt a pipe, a file, or CI logs.
func (u *UI) StartProgress(size int64, prefix string) Progress {
	if !u.Mode().ShowProgress {
		return noProgress{}
	}
	b := &bar{}
	b.stop = u.drawer.draw(newBarModel(prefix, size, &b.read))
	return b
}

// Spin shows a spinner on the error stream until the returned func is
// called. Like StartProgress, it does nothing unless the Mode shows progress.
func (u *UI) Spin(suffix string) func() {
	if !u.Mode().ShowProgress {
		return func() {}
	}
	return u.drawer.draw(newSpinModel(suffix))
}

// drawer draws the widgets that the Mode of a UI allows: with programs on
// stderr, or not at all in tests, which count them
type drawer interface {
	draw(w widget) (stop func())
}

// programs is the drawer of a UI: each widget is a Bubble Tea program
type programs struct {
	u *UI
}

// draw draws a widget on stderr until the returned func stops it, which
// wipes the line and waits for the program to exit. Without input, stdin is
// never put in raw mode. Without a context, nothing can kill the program
// before its final render, the wipe. Send blocks until the program has
// started, so one that is stopped at once still shuts down; after it has
// exited, Send and Wait return at once, so stopping twice is harmless.
func (p programs) draw(w widget) (stop func()) {
	prog := tea.NewProgram(statusModel{widget: w}, append(p.u.programOptions(),
		tea.WithOutput(p.u.session.Err),
		tea.WithInput(nil),
	)...)
	go prog.Run()
	return func() {
		prog.Send(doneMsg{})
		prog.Wait()
	}
}

// A widget draws the one line of a status program, frame by frame, and
// tells the terminal its progress, or nil for none
type widget interface {
	Init() tea.Cmd
	Update(tea.Msg) (widget, tea.Cmd)
	Content() string
	Native() *tea.ProgressBar
}

// statusModel shows a widget until it is told it is done, when its view is
// empty, so that the final render wipes the line and leaves the cursor at
// its start, as the legacy widgets did. The cursor is left to the renderer
// (hidden while shown): placing it would also set its shape, which some
// terminals keep after the program exits.
type statusModel struct {
	widget
	done bool
}

type (
	tickMsg struct{}
	doneMsg struct{}
)

func (m statusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(doneMsg); ok {
		m.done = true
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.widget, cmd = m.widget.Update(msg)
	return m, cmd
}

func (m statusModel) View() tea.View {
	var v tea.View
	if !m.done {
		v.SetContent(m.Content())
		v.ProgressBar = m.Native()
	}
	v.DisableBracketedPasteMode = true // Nothing is read, so no pastes
	return v
}

// Progress is a bar that StartProgress has started: it counts what is read
// through NewProxyReader towards its size, until Finish stops it
type Progress interface {
	NewProxyReader(io.Reader) io.Reader
	Finish()
}

// bar is a Progress drawn by a running Bubble Tea program
type bar struct {
	stop func()
	read atomic.Int64
}

// NewProxyReader counts the bytes read through it towards the bar
func (b *bar) NewProxyReader(r io.Reader) io.Reader {
	return &countingReader{Reader: r, read: &b.read}
}

// Finish stops the bar's program, as draw describes
func (b *bar) Finish() {
	b.stop()
}

// countingReader adds what it reads to a counter
type countingReader struct {
	io.Reader
	read *atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read.Add(int64(n))
	return n, err
}

// barModel is the widget of a bar: the program ticks it and tells it the
// terminal width, and Content draws each frame
type barModel struct {
	prefix string
	total  int64
	read   *atomic.Int64
	width  int // of the terminal; 0 until known
	frame  int // index into tips
}

func newBarModel(prefix string, total int64, read *atomic.Int64) barModel {
	return barModel{prefix: prefix, total: total, read: read}
}

// Native is the bar as the terminal shows it: its percentage, whole and at
// most 100, or busy without one when the total is not known
func (m barModel) Native() *tea.ProgressBar {
	if m.total <= 0 {
		return tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, int(m.read.Load()*100/m.total))
}

func (m barModel) Init() tea.Cmd {
	return tick()
}

func (m barModel) Update(msg tea.Msg) (widget, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		m.frame = (m.frame + 1) % len(tips)
		return m, tick()
	}
	return m, nil
}

// Content is one frame of the bar. On a narrow terminal the bar shrinks to
// its brackets, then is dropped, then the line is cut, as pb did.
func (m barModel) Content() string {
	value := m.read.Load() // once, so the percentage and the bar agree
	width := m.width
	if width <= 0 {
		width = defaultWidth
	}

	percent := "?%"
	if m.total > 0 {
		percent = fmt.Sprintf("%.02f%%", float64(value)/(float64(m.total)/100))
	}

	// The bar gets the width that the rest of the line leaves
	rest := " " + percent
	barWidth := width - ansi.StringWidth(m.prefix) - len(rest)
	if barWidth <= 0 {
		return ansi.Truncate(m.prefix+rest, width, "")
	}

	return m.prefix + m.renderBar(barWidth, value) + rest
}

// renderBar is pb's "bar" element in a given number of cells
func (m barModel) renderBar(width int, value int64) string {
	switch width {
	case 1:
		return "["
	case 2:
		return "[]"
	}
	inner := width - 2

	// Filled cells, rounded up, so the bar moves off the start at once. As
	// in pb, a negative total counts as its magnitude here, though it has
	// no percentage, and an overrun fills the bar.
	count := 0
	if total := max(m.total, -m.total); total > 0 {
		count = int(math.Ceil(float64(min(value, total)) / float64(total) * float64(inner)))
	}

	// The tip replaces the last filled cell, even of a full bar: pb left
	// it out only on the frame after Finish, which CleanOnFinish wiped
	filled := ""
	if count > 0 {
		filled = strings.Repeat("=", count-1) + string(tips[m.frame])
	}

	return "[" + filled + strings.Repeat(" ", inner-count) + "]"
}

// spinModel is the widget of a spinner: the glyph, then the suffix
type spinModel struct {
	spin   spinner.Model
	suffix string
}

func newSpinModel(suffix string) spinModel {
	spin := spinner.New(
		spinner.WithSpinner(spinSet),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.White)), // as briandowns/spinner
	)
	return spinModel{spin: spin, suffix: suffix}
}

func (m spinModel) Init() tea.Cmd {
	return m.spin.Tick
}

func (m spinModel) Update(msg tea.Msg) (widget, tea.Cmd) {
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	return m, cmd
}

func (m spinModel) Content() string {
	return m.spin.View() + m.suffix
}

// Native is nil: a spinner tells the terminal nothing, as the wait it shows
// may be for the user, which a busy tab would misreport
func (m spinModel) Native() *tea.ProgressBar {
	return nil
}

type noProgress struct{}

func (np noProgress) NewProxyReader(r io.Reader) io.Reader {
	return r
}

func (np noProgress) Finish() {
	// noop
}
