// Package keymap holds key bindings as data: commands declared in code, each with an id, a scope and
// default keys, and key-mapping schemes (files) that change those keys. A [Map] is the commands with the
// schemes layered on them, validated and immutable; the tui App holds one and every widget asks it
// which of its commands a key is.
//
// A scheme is Qt Creator's keyboard-mapping scheme, in a .xml file:
//
//	<!DOCTYPE KeyboardMappingScheme>
//	<mapping>
//	  <shortcut id="tui.list.down"><key value="Down"/><key value="Ctrl+N"/></shortcut>
//	  <shortcut id="tui.list.top"><key value=""/></shortcut>   <!-- unbound -->
//	</mapping>
//
// It imports nothing from tui (only tui/key), so tui can hold a *Map.
//
// REFERENCE: https://doc.qt.io/qtcreator/creator-how-to-change-keyboard-shortcuts.html
package keymap

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/key"
)

// Scope is where a command's keys must be unique. Two scopes are OUTER: Global (the Window's) and App
// (framework traversal, live even inside a modal trap). Every other scope is a widget's, a holder's or
// a product's own: "tui.list", "tui.editor.vim.normal", "autodoc.todos".
type Scope string

const (
	Global Scope = "global"
	App    Scope = "tui.app"
)

// outer reports whether s is consulted after every widget and holder.
func (s Scope) outer() bool { return s == Global || s == App }

// Command is one bindable operation, declared in code (Qt Creator's name for it). A scheme can change
// its keys, never its id or its scope.
type Command struct {
	ID    string    // "tui.list.down": the scheme's <shortcut id>
	Scope Scope     // where its keys must be unique
	Keys  []key.Seq // the defaults, used where no scheme names ID
	Text  string    // an i18n message id describing it, for help cards; "" is none
}

// SourceKind says where a binding's keys came from.
type SourceKind uint8

const (
	// FromDefault: the command's declared keys; no scheme named it.
	FromDefault SourceKind = iota
	// FromScheme: a scheme file's <shortcut>, at File and Line.
	FromScheme
	// FromOverlay: a host's Go code (an Editor's WithKeymap overlay), reported by a query, never by Build.
	FromOverlay
)

// Source is where a binding's keys came from.
type Source struct {
	Kind SourceKind
	File string // the scheme, for FromScheme
	Line int
}

// Binding is a command and its keys, as a help card or a query reports it.
type Binding struct {
	Command    Command
	Keys       []key.Seq
	Scope      Scope
	Source     Source
	Reaches    []key.Seq // the event chords that reach this binding at its stop (a query fills it)
	ShadowedBy []string  // the commands that win the rest of key.Accepts(Keys), earlier in routing
}

// ProblemKind names one finding of Build or a check.
type ProblemKind uint8

const (
	// Duplicate: a command id declared twice.
	Duplicate ProblemKind = iota
	// UnknownID: a scheme names a command no one declared.
	UnknownID
	// Conflict: two commands of one scope share a key.
	Conflict
	// Shadow (a warning): an inner scope's key overlaps a Global or App key, which the inner stop wins
	// while focus is there.
	Shadow
	// Malformed: a scheme the parser refused.
	Malformed
	// FixedKey: a fixed key overlaps a command key of its holder's scope (a check, not Build).
	FixedKey
)

func (k ProblemKind) String() string {
	switch k {
	case Duplicate:
		return "duplicate"
	case UnknownID:
		return "unknown id"
	case Conflict:
		return "conflict"
	case Shadow:
		return "shadow"
	case Malformed:
		return "malformed"
	case FixedKey:
		return "fixed key"
	}
	return "problem"
}

// Problem is one finding, with the scheme position when a scheme caused it.
type Problem struct {
	File string
	Line int
	ID   string
	Kind ProblemKind
	Key  key.Seq // the key in question, for a Conflict, Shadow or FixedKey
	With string  // the other command (or fixed key) of a Conflict, Shadow or FixedKey
}

// Warning reports whether p is a warning rather than an error.
func (p Problem) Warning() bool { return p.Kind == Shadow }

func (p Problem) String() string {
	var at string
	if p.File != "" {
		at = p.File
		if p.Line > 0 {
			at += fmt.Sprintf(", line %d", p.Line)
		}
		at += ": "
	}
	switch p.Kind {
	case Duplicate:
		return fmt.Sprintf("%scommand %q is declared twice", at, p.ID)
	case UnknownID:
		return fmt.Sprintf("%sno command %q is declared", at, p.ID)
	case Conflict:
		return fmt.Sprintf("%s%s is bound to both %q and %q in one scope", at, p.Key, p.ID, p.With)
	case Shadow:
		return fmt.Sprintf("%s%s of %q is also %q's, which wins while %q's scope has focus", at, p.Key, p.With, p.ID, p.ID)
	case FixedKey:
		return fmt.Sprintf("%sthe fixed key %s overlaps %s of %q", at, p.With, p.Key, p.ID)
	}
	return at + p.ID
}

// Error is Build's refusal: every Problem that is not a warning, so one save shows all. It wraps
// errs.ErrInvalidArgument, as i18n's refusals do.
type Error struct{ Problems []Problem }

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.String()
	}
	return "keymap: " + strings.Join(lines, "; ")
}

func (e *Error) Unwrap() error { return errs.ErrInvalidArgument }
