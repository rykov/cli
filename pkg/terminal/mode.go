package terminal

// Caps says which streams of a session a person is at: a terminal, rather
// than a pipe, a file, or nothing
type Caps struct {
	InTTY  bool // Stdin is a terminal
	OutTTY bool // Stdout is a terminal
	ErrTTY bool // Stderr is a terminal
}

// Options are the global flags that shape what is shown and asked
type Options struct {
	Yes        bool // Answer yes to every confirmation
	NoInput    bool // Never ask; fail where input is needed
	Quiet      bool // Show only results, warnings, and errors
	NoProgress bool // Show no bars or spinners
}

// Mode is how a command shows and asks, decided from the streams a person
// is at and the options
type Mode struct {
	Interactive  bool // A person is at stdin and at stderr, where questions are shown, and asking is on
	OutTTY       bool // A person reads stdout as it is printed
	ShowInfo     bool // What is not a result is printed
	ShowProgress bool // Bars and spinners are drawn
	AutoYes      bool // Every confirmation is answered yes
}

// decide is the Mode of a session with the given options. Questions, bars
// and spinners are shown on stderr, as huh shows a form by default, so
// each needs a person there: stdout may be piped, and stays the result
// alone.
func decide(c Caps, o Options) Mode {
	return Mode{
		Interactive:  c.InTTY && c.ErrTTY && !o.NoInput,
		OutTTY:       c.OutTTY,
		ShowInfo:     !o.Quiet,
		ShowProgress: c.ErrTTY && !o.Quiet && !o.NoProgress,
		AutoYes:      o.Yes,
	}
}
