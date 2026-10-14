package widget

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// THE COMMAND LINE — the verbs an editor runs from its ':' prompt, and RunCommand, which a host, a
// script or an agent calls with the same line a user types.
//
// A Commands registry holds the verbs. Every core starts with DefaultCommands; a host composes its
// own in (Commands().Add, or Set to replace one), or gives the core a registry of its own
// (CoreCommands). Saving and quitting are the host's: w, wq and x call the CoreOnWrite hook, q the
// CoreOnQuit hook, and with no hook they answer ErrNoWriter / ErrNoQuit.

// ErrUnknownCommand is RunCommand's answer to a verb the registry does not have.
var ErrUnknownCommand = errors.New("widget: not an editor command")

// ErrCommandExists is Add's answer to a name or alias already registered.
var ErrCommandExists = errors.New("widget: an editor command by that name exists")

// ErrNoWriter is w's answer when the host gave the core no way to save (CoreOnWrite).
var ErrNoWriter = errors.New("widget: this editor cannot save")

// ErrNoQuit is q's answer when the host gave the core no way to quit (CoreOnQuit).
var ErrNoQuit = errors.New("widget: this editor cannot quit")

// Command is one verb of the command line.
type Command struct {
	Name    string   // the verb as typed: "w", "find"; letters, digits, '-' and '_'
	Aliases []string // other spellings: "write" for "w"
	Args    string   // the arguments, for completion's help: "<term> <with>"
	Help    string   // what it does, in one line
	// Run does it. A returned error is the prompt's to show; the line can then be corrected.
	Run func(CommandContext) error
	// Complete is what Tab offers for an argument: the candidates for the word being typed. The
	// prompt keeps those that start with it, in Complete's order, so Complete may return them all.
	// nil leaves Tab listing Args and Help, as for a command without it.
	Complete func(CompleteContext) []string
}

// CompleteContext is what a command's Complete gets: the line typed so far, up to the word Tab
// completes.
type CompleteContext struct {
	Core *EditorCore // the editor
	Name string      // the verb as typed (an alias, say), without the bang
	Bang bool        // the verb ended in '!'
	Args []string    // the words before the one being completed
	Word string      // the word being completed, as typed so far: "" after a space
}

// CommandContext is what a command runs with.
type CommandContext struct {
	Core *EditorCore // the editor: every core function
	Name string      // the verb as typed (an alias, say), without the bang
	Bang bool        // the verb ended in '!': q!
	Args []string    // the words after the verb
	Line string      // everything after the verb, trimmed at its ends only
}

// Commands is a command registry: an editor's verbs, in the order they were added.
type Commands struct {
	order  []*Command
	byName map[string]*Command // every name and alias
}

// NewCommands is a registry holding cmds; a name given twice is ErrCommandExists.
func NewCommands(cmds ...Command) (*Commands, error) {
	s := &Commands{byName: map[string]*Command{}}
	return s, s.Add(cmds...)
}

func validName(n string) bool {
	if n == "" {
		return false
	}
	for _, r := range n {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	// a verb made of digits would read as a line number
	_, err := strconv.Atoi(n)
	return err != nil
}

func (cmd Command) names() []string { return append([]string{cmd.Name}, cmd.Aliases...) }

func (cmd Command) check() error {
	if cmd.Run == nil {
		return fmt.Errorf("widget: editor command %q has no Run", cmd.Name)
	}
	for _, n := range cmd.names() {
		if !validName(n) {
			return fmt.Errorf("widget: %q is not a valid editor command name", n)
		}
	}
	return nil
}

// Add registers cmds. A name or alias already registered is ErrCommandExists, and nothing of that
// command is added: composing two sets that both define a verb is a decision for Set.
func (s *Commands) Add(cmds ...Command) error {
	var errs []error
	for _, cmd := range cmds {
		if err := cmd.check(); err != nil {
			errs = append(errs, err)
			continue
		}
		clash := false
		for _, n := range cmd.names() {
			if _, ok := s.byName[n]; ok {
				errs = append(errs, fmt.Errorf("%w: %s", ErrCommandExists, n))
				clash = true
			}
		}
		if !clash {
			s.put(cmd)
		}
	}
	return errors.Join(errs...)
}

// Set registers cmds, replacing every command that has one of their names or aliases.
func (s *Commands) Set(cmds ...Command) error {
	var errs []error
	for _, cmd := range cmds {
		if err := cmd.check(); err != nil {
			errs = append(errs, err)
			continue
		}
		s.Remove(cmd.names()...)
		s.put(cmd)
	}
	return errors.Join(errs...)
}

func (s *Commands) put(cmd Command) {
	c := &cmd
	s.order = append(s.order, c)
	for _, n := range cmd.names() {
		s.byName[n] = c
	}
}

// Remove unregisters the commands that have any of names as a name or alias.
func (s *Commands) Remove(names ...string) {
	for _, n := range names {
		c, ok := s.byName[n]
		if !ok {
			continue
		}
		for _, m := range c.names() {
			delete(s.byName, m)
		}
		for i, o := range s.order {
			if o == c {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
}

// Lookup is the command with name as its name or an alias.
func (s *Commands) Lookup(name string) (Command, bool) {
	if c, ok := s.byName[name]; ok {
		return *c, true
	}
	return Command{}, false
}

// All is every command, in the order they were added.
func (s *Commands) All() []Command {
	out := make([]Command, len(s.order))
	for i, c := range s.order {
		out[i] = *c
	}
	return out
}

// Complete is every command whose name or an alias starts with prefix, in order: what Tab offers.
func (s *Commands) Complete(prefix string) []Command {
	var out []Command
	for _, c := range s.order {
		for _, n := range c.names() {
			if strings.HasPrefix(n, prefix) {
				out = append(out, *c)
				break
			}
		}
	}
	return out
}

// CoreCommands gives the core a registry of its own in place of DefaultCommands; nil leaves it
// with none, so ':' is not consumed.
func CoreCommands(s *Commands) CoreOption { return func(c *EditorCore) { c.commands = s } }

// Commands is the core's registry, for a host to add to; nil when it has none.
func (c *EditorCore) Commands() *Commands { return c.commands }

// CoreOnWrite is how w, wq and x save: the host writes the text where it belongs and, when quit is
// true, quits once the write succeeded (its write may finish later: a Save As dialog, a daemon).
// The host calls MarkSaved after a successful write.
func CoreOnWrite(fn func(quit bool) error) CoreOption { return func(c *EditorCore) { c.onWrite = fn } }

// CoreOnQuit is how q quits; force is q!'s bang. What an unforced quit over unsaved work does is
// the host's choice: ask, or refuse as vim does.
func CoreOnQuit(fn func(force bool) error) CoreOption { return func(c *EditorCore) { c.onQuit = fn } }

// SetOnWrite and SetOnQuit set the hooks after construction.
func (c *EditorCore) SetOnWrite(fn func(quit bool) error) { c.onWrite = fn }
func (c *EditorCore) SetOnQuit(fn func(force bool) error) { c.onQuit = fn }

// Write asks the host to save (CoreOnWrite), quitting after when quit.
func (c *EditorCore) Write(quit bool) error {
	if c.onWrite == nil {
		return ErrNoWriter
	}
	return c.onWrite(quit)
}

// Quit asks the host to quit (CoreOnQuit); force discards unsaved changes.
func (c *EditorCore) Quit(force bool) error {
	if c.onQuit == nil {
		return ErrNoQuit
	}
	return c.onQuit(force)
}

// RunCommand runs one command line, as the ':' prompt does (no leading ':'):
//
//	42          go to line 42, counted from 1 (0 is the first line; past the last is refused)
//	verb args   the registry's verb, with its arguments; "verb!" sets Bang
//
// A command runs inside a Batch: whatever it edits is one undo step. An empty line does nothing. A
// verb the registry does not have is ErrUnknownCommand.
func (c *EditorCore) RunCommand(line string) error {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ":"))
	if line == "" {
		return nil
	}
	if n, err := strconv.Atoi(line); err == nil {
		return c.goToLineNumber(n)
	}
	verb, rest, _ := strings.Cut(line, " ")
	bang := strings.HasSuffix(verb, "!")
	verb = strings.TrimSuffix(verb, "!")
	if c.commands == nil {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, verb)
	}
	cmd, ok := c.commands.Lookup(verb)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, verb)
	}
	rest = strings.TrimSpace(rest)
	x := CommandContext{Core: c, Name: verb, Bang: bang, Args: strings.Fields(rest), Line: rest}
	// one command, one undo step, whatever it edits
	return c.Batch(func() error { return cmd.Run(x) })
}

// goToLineNumber moves the cursor to the first non-blank of line n, counted from 1.
func (c *EditorCore) goToLineNumber(n int) error {
	total := len(c.buf.lines)
	if n < 0 || n > total {
		return fmt.Errorf("line %d: the document has %d line%s", n, total, plural(total))
	}
	c.setJumpMark()
	ln := max(n-1, 0)
	col := 0
	for _, cl := range c.buf.lineClusters(ln) {
		if cl != " " && cl != "\t" {
			break
		}
		col++
	}
	c.SetLine(ln, col)
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// DefaultCommands is every core's starting set:
//
//	<n>                     go to line n
//	w, write                save
//	q, quit / q!            quit / quit, discarding unsaved changes
//	wq                      save, then quit
//	x, exit                 save if modified, then quit
//	find <term>             mark term and go to it; n and N go on
//	replace <term> <with>   replace each match, asking y / n (every match, with no one to ask)
//	nohl                    clear the find's marks
func DefaultCommands() []Command {
	return []Command{
		{Name: "w", Aliases: []string{"write"}, Help: "save", Run: func(x CommandContext) error { return x.Core.Write(false) }},
		{Name: "q", Aliases: []string{"quit"}, Help: "quit; q! discards unsaved changes", Run: func(x CommandContext) error { return x.Core.Quit(x.Bang) }},
		{Name: "wq", Help: "save, then quit", Run: func(x CommandContext) error { return x.Core.Write(true) }},
		{Name: "x", Aliases: []string{"exit"}, Help: "save if modified, then quit", Run: func(x CommandContext) error {
			if !x.Core.Modified() {
				return x.Core.Quit(false)
			}
			return x.Core.Write(true)
		}},
		{Name: "find", Args: "<term>", Help: "find term; n and N go to the next and previous", Run: findCommand},
		{Name: "replace", Args: "<term> <with>", Help: "replace each match, asking y / n; q or Esc stops", Run: replaceCommand},
		{Name: "nohl", Help: "clear the find's marks", Run: func(x CommandContext) error {
			x.Core.SetFind("", FindOptions{})
			return nil
		}},
	}
}

// findCommand: the whole line is the term, spaces included; a term all in lower case folds case.
func findCommand(x CommandContext) error {
	term := x.Line
	if term == "" {
		return errors.New("find: what? find <term>")
	}
	o := FindOptions{IgnoreCase: term == strings.ToLower(term)}
	x.Core.SetFind(term, o)
	if !x.Core.FindNext(false) {
		return fmt.Errorf("find: no %q", term)
	}
	return nil
}

// replaceCommand: the first word is the term, the rest of the line what replaces it. With a
// widget to ask (SetReplaceAsk) it asks at each match; otherwise it replaces every match.
func replaceCommand(x CommandContext) error {
	term, with, ok := strings.Cut(x.Line, " ")
	if !ok || term == "" {
		return errors.New("replace: replace <term> <with>")
	}
	with = strings.TrimSpace(with)
	o := FindOptions{IgnoreCase: term == strings.ToLower(term)}
	if len(x.Core.FindAll(term, o)) == 0 {
		return fmt.Errorf("replace: no %q", term)
	}
	if x.Core.replaceAsk != nil {
		s, _ := x.Core.StartReplace(term, with, o)
		return x.Core.replaceAsk(s)
	}
	_, err := x.Core.ReplaceAll(term, with, o)
	return err
}

// SetReplaceAsk is how a widget asks at each match of replace: ask shows the question (the
// CommandLine's confirm card) and answers the session as the user does. nil replaces every match
// unasked.
func (c *EditorCore) SetReplaceAsk(ask func(*ReplaceSession) error) { c.replaceAsk = ask }

// defaultCommands is a fresh registry of DefaultCommands, which never clash.
func defaultCommands() *Commands {
	s, _ := NewCommands(DefaultCommands()...)
	return s
}

// SetCommandLine is how a widget with a ':' prompt answers ActCommandLine: open reports whether
// the prompt opened. With none, or with no registry, ':' is not consumed.
func (c *EditorCore) SetCommandLine(open func() bool) { c.commandLine = open }
