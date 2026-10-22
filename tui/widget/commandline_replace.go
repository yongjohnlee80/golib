package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// THE REPLACE QUESTION: :s///c's card, asking at each match whether to replace it.

// replaceChoice is an answer to the replace question.
type replaceChoice uint8

const (
	replaceNone replaceChoice = iota // k is none of the question's keys
	replaceYes                       // replace this match, and ask at the next
	replaceNo                        // keep this match, and ask at the next
	replaceQuit                      // stop asking
)

// String is the answer in a word: "yes", "no", "quit", "" for none, or "unknown" past them.
func (c replaceChoice) String() string {
	if c > replaceQuit {
		return "unknown"
	}
	return [...]string{"", "yes", "no", "quit"}[c]
}

// replaceAnswer is the replace question's answer k is: the command k runs, by its id.
func replaceAnswer(ctx tui.KeyResolver, k tui.KeyEvent) replaceChoice {
	cmd, ok := ctx.KeyCommand(k, cmdlineReplaceCommands...)
	if !ok {
		return replaceNone
	}
	switch cmd.ID {
	case CmdlineReplaceYes.ID:
		return replaceYes
	case CmdlineReplaceNo.ID:
		return replaceNo
	case CmdlineReplaceQuit.ID:
		return replaceQuit
	}
	return replaceNone
}

// askReplace asks at each match of s, one card at a time, answered by y, n, q and Esc.
func (l *CommandLine) askReplace(s *ReplaceSession) error {
	host, ok := l.host()
	if !ok || !l.on {
		// nowhere to ask, or no prompt: every match, as with no widget
		for more := true; more; {
			var err error
			if more, err = s.Answer(true); err != nil {
				return err
			}
		}
		return nil
	}
	var m *Modal
	question := NewText("", WithTextStyle(cardText()))
	question.SetTextMessage(tui.Msg("tui.cmdline.replaceQuestion"))
	m = NewModal(question,
		WithModalTitleMessage(tui.Msg("tui.cmdline.replaceTitle")),
		WithPlacement(PlacementTopRight), // clear of the notifications at the bottom
		WithScrim(false),
		WithModalKeys(func(k tui.KeyEvent) bool {
			switch a := replaceAnswer(m.Context(), k); a {
			case replaceYes, replaceNo:
				more, err := s.Answer(a == replaceYes)
				if err != nil || !more {
					m.Dismiss(DismissAccept)
				}
				return true
			case replaceQuit:
				m.Dismiss(DismissCancel)
				return true
			}
			return false
		}),
		WithModalKeyBinder(func(km *keymap.Map) []keymap.Binding {
			return tui.BindingsOf(km, cmdlineReplaceCommands...)
		}),
		WithOnDismiss(func(DismissReason) {
			s.Stop()
			if l.ask == m {
				l.ask = nil
			}
		}))
	if err := m.openOn(host); err != nil {
		s.Stop()
		return err
	}
	l.ask = m
	return nil
}
