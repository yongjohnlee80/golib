package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// THE REPLACE QUESTION: :s///c's card, asking at each match whether to replace it.

// replaceAnswer is the replace question's answer k is: "yes", "no", "quit", or "" for none.
func replaceAnswer(ctx tui.KeyResolver, k tui.KeyEvent) string {
	cmd, ok := ctx.KeyCommand(k, cmdlineReplaceCommands...)
	if !ok {
		return ""
	}
	return strings.TrimPrefix(cmd.ID, "tui.cmdline.replace.")
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
			case "yes", "no":
				more, err := s.Answer(a == "yes")
				if err != nil || !more {
					m.Dismiss(DismissAccept)
				}
				return true
			case "quit":
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
	if err := m.Open(host); err != nil {
		s.Stop()
		return err
	}
	l.ask = m
	return nil
}
