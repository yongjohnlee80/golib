package widget

import tuiwidget "github.com/yongjohnlee80/golib/tui/widget"

// THE ':' PROMPT — the terminal Editor's own CommandLine module, opened over this editor's body:
// the same verbs, Tab completion and replace questions, drawn by this backend.
//
// # Command-Line Event Flow & Overlay Architecture
//
// Below is the interaction between the editor key handling, the command-line overlay,
// and the command registry execution:
//
//	                 ┌─────────────────────────────┐
//	                 │       User Key Event        │
//	                 └──────────────┬──────────────┘
//	                                │
//	                 ┌──────────────┴──────────────┐
//	                 │  Is ':' and CommandLineOn?  │
//	                 └──────┬───────────────┬──────┘
//	                   Yes  │               │ No / Prompt Open
//	                        ▼               ▼
//	             ┌────────────────────┐   ┌────────────────────┐
//	             │ CommandLine().Open │   │ Route to cmdline / │
//	             │ - Mount overlay    │   │ editor core body   │
//	             │ - Steal key focus  │   └─────────┬──────────┘
//	             └────────────────────┘             │ <Enter>
//	                                                ▼
//	                                      ┌────────────────────┐
//	                                      │ RunCommand(line)   │
//	                                      │ - Execute verb     │
//	                                      │ - Dismiss prompt   │
//	                                      └────────────────────┘

// CommandLine is the editor's ':' prompt, the terminal Editor's module: on unless turned off
// (CommandLine().SetEnabled(false)).
func (e *Editor) CommandLine() *tuiwidget.CommandLine { return e.cmdline }
