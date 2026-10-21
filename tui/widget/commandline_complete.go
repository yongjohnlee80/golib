package widget

import "strings"

// THE COMMAND LINE'S COMPLETION: Tab completes the verb from the core's Commands, then its
// arguments from the command's own completer, offering the candidates in the dropdown.

// completion is Tab's walk through the candidates: what was typed when it began, what precedes
// every candidate in the field (the verb and the words before an argument; "" for a verb), each
// candidate and its row in the dropdown, and which one the last Tab chose (-1: only the common
// prefix so far).
type completion struct {
	typed string
	head  string
	cands []string
	rows  []string
	at    int
}

// complete completes the word being typed: the verb, from the core's Commands, or after it an
// argument, from the command's Complete. The first Tab fills in what every candidate shares (and a
// space when there is one candidate) and lists them; each Tab after that takes the next candidate
// in turn, Shift+Tab the previous. After the verb of a command without Complete, Tab lists its
// arguments and help.
func (l *CommandLine) complete(back bool) {
	line := l.field.Value()
	if l.comp.cands == nil {
		verb, rest, spaced := strings.Cut(line, " ")
		if spaced {
			l.completeArg(line, verb, rest, back)
			return
		}
		cmds := l.core.Commands().Complete(line)
		if len(cmds) == 0 {
			l.list.say("no command starts with " + quoteOrNothing(line))
			return
		}
		names := make([]string, len(cmds))
		for i, c := range cmds {
			names[i] = c.Name
		}
		if !l.offer(completion{typed: line, cands: names, rows: describeEach(cmds)}, describe(cmds[0])) {
			return
		}
	}
	l.comp.at = walkStep(l.comp.at, len(l.comp.cands), back)
	l.list.choose(l.comp.at)
	l.setField(l.comp.head + l.comp.cands[l.comp.at])
}

// offer starts Tab's walk through c's candidates, typed being c.typed's word. One candidate is
// filled in whole, with a space, and once is said: done, offer says false. Several are listed,
// and what they share is filled in: offer says false when that moved the field, true when the walk
// takes the first Tab on to the first candidate.
func (l *CommandLine) offer(c completion, once string) bool {
	c.at = -1
	l.comp = c
	if len(c.cands) == 1 {
		l.list.say(once)
		l.setField(c.head + c.cands[0] + " ")
		l.comp.cands = nil // done: the next Tab completes the next word
		return false
	}
	l.list.set(c.rows, -1)
	typed := strings.TrimPrefix(c.typed, c.head)
	if p := commonPrefix(c.cands, typed); p != typed {
		l.setField(c.head + p)
		return false
	}
	return true
}

// completeArg completes the argument being typed after verb: rest is the line after the verb's
// space. A command without Complete, or whose candidates none starts with the word, has Tab say
// its arguments and help instead.
func (l *CommandLine) completeArg(line, verb, rest string, back bool) {
	name := strings.TrimSuffix(verb, "!")
	cmd, ok := l.core.Commands().Lookup(name)
	if !ok {
		return
	}
	if cmd.Complete == nil {
		l.list.say(describe(cmd))
		return
	}
	words := strings.Fields(rest)
	word := ""
	if len(words) > 0 && !strings.HasSuffix(rest, " ") {
		word, words = words[len(words)-1], words[:len(words)-1]
	}
	seen := map[string]bool{}
	var cands []string
	for _, c := range cmd.Complete(CompleteContext{Core: l.core, Name: name, Bang: name != verb, Args: words, Word: word}) {
		if c != "" && strings.HasPrefix(c, word) && !seen[c] {
			seen[c] = true
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		l.list.say(describe(cmd))
		return
	}
	head := line[:len(line)-len(word)]
	if l.offer(completion{typed: line, head: head, cands: cands, rows: cands}, describe(cmd)) {
		l.complete(back) // several that share nothing more: this Tab takes the first (or the last)
	}
}

// setField puts s in the field. SetValue is not typing (WithOnEdit does not run), so the
// completion and its list stay.
func (l *CommandLine) setField(s string) { l.field.SetValue(s) }

// describe is one candidate's line: "replace <term> <with> — replace each match, …".
func describe(c Command) string {
	s := c.Name
	if c.Args != "" {
		s += " " + c.Args
	}
	if c.Help != "" {
		s += " — " + c.Help
	}
	return s
}

func describeEach(cs []Command) []string {
	rows := make([]string, len(cs))
	for i, c := range cs {
		rows[i] = describe(c)
	}
	return rows
}

func quoteOrNothing(s string) string {
	if s == "" {
		return "that"
	}
	return "\"" + s + "\""
}
