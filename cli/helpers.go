package cli

import (
	"github.com/charmbracelet/x/ansi"

	"fmt"
	"io"
	"strings"
	"time"
)

// printTable prints rows as columns two spaces apart, as text/tabwriter
// does, but measures each cell as shown: without its colour sequences, and
// by the cells a terminal gives its runes (two for CJK and emoji), so that
// a styled or wide cell keeps its column aligned. The last cell of a row is
// not padded.
func printTable(w io.Writer, rows [][]string) {
	width := ansi.StringWidth

	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], width(cell))
		}
	}

	var out strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			out.WriteString(cell)
			if i < len(row)-1 {
				out.WriteString(strings.Repeat(" ", widths[i]-width(cell)+2))
			}
		}
		out.WriteString("\n")
	}
	io.WriteString(w, out.String())
}

// timeString is a time as printed in a table, in the zone of the terminal.
// At a terminal it has how long ago it was, when under a day. Piped, it is
// RFC 3339, for a program to parse in any zone, and has no "ago", which
// would give some rows more columns.
func timeString(t time.Time, loc *time.Location, tty bool) string {
	if t.IsZero() {
		return "N/A" // Like the other fields the API leaves out
	}

	zoned := t.In(loc)
	if !tty {
		return zoned.Format(time.RFC3339)
	}

	out := zoned.Format("2006-01-02 15:04")
	// A time ahead of the clock, by skew, has no "ago"
	if ago := time.Since(t); ago >= 0 && ago < 24*time.Hour {
		out += fmt.Sprintf(" (~ %s ago)", agoString(ago))
	}
	return out
}

// agoString is how long ago, in its largest whole unit: 2h for 2h30m
func agoString(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// packageKinds lists the package kinds of the API, for help only. They are
// not validated here, so a new kind can be used before a release lists it.
const packageKinds = "ruby, js, python, php, deb, rpm, bower, nuget, maven, go"

// splitPackageVersion splits an argument of PACKAGE@VERSION
// into its two parts, neither of which may be empty
func splitPackageVersion(arg string) (pkg, ver string, ok bool) {
	at := strings.LastIndex(arg, "@")
	if at <= 0 || at == len(arg)-1 {
		return "", "", false
	}
	return arg[:at], arg[at+1:], true
}
