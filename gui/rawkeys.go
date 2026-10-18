package gui

import (
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/gio/io/event"
	"github.com/yongjohnlee80/gio/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// Raw keys: a game's keyboard. In text mode (the default) printable keys arrive as text, through
// the input method. In raw mode every key, letters, space and lone modifiers included, arrives
// as a press and a release, and the input method is off.

// SetRawKeys switches the window's keyboard between text input and raw keys; typically a game
// view calls it as it gains and loses focus. App goroutine; the Gio goroutine applies it on its
// next frame.
func (b *Backend) SetRawKeys(on bool) {
	if b.raw.Swap(on) != on && b.win != nil {
		b.win.Invalidate()
	}
}

// rawModifiers are the lone modifier keys, by Gio's names. Gio names no side, so each is the
// left key.
var rawModifiers = map[key.Name]rune{
	key.NameShift:   tui.KeyLeftShift,
	key.NameCtrl:    tui.KeyLeftControl,
	key.NameAlt:     tui.KeyLeftAlt,
	key.NameSuper:   tui.KeyLeftSuper,
	key.NameCommand: tui.KeyLeftSuper,
}

// rawKeyOf is a key as raw mode reports it: its tui code (a named key, a lone modifier, or the
// key's lower-case rune), pressed or released, with the modifiers held. It goes through the
// platform contract's KeyInput, as every toolkit key does.
func rawKeyOf(in KeyInput) (tui.KeyEvent, bool) {
	kind := tui.KeyRelease
	if in.Press {
		kind = tui.KeyPress
	}
	name := key.Name(in.Name)
	if code, ok := named[name]; ok {
		return tui.KeyEvent{Kind: kind, Code: code, Mods: in.Mods}, true
	}
	if code, ok := rawModifiers[name]; ok {
		return tui.KeyEvent{Kind: kind, Code: code, Mods: in.Mods}, true
	}
	if name == key.NameSpace {
		return tui.KeyEvent{Kind: kind, Code: ' ', Mods: in.Mods}, true
	}
	r, size := utf8.DecodeRuneInString(in.Name)
	if size == 0 || size != len(in.Name) || !unicode.IsPrint(r) {
		return tui.KeyEvent{}, false
	}
	return tui.KeyEvent{Kind: kind, Code: unicode.ToLower(r), Mods: in.Mods}, true
}

// applyRawMode brings the Gio goroutine to the mode SetRawKeys last asked for. Entering raw mode
// drops any composition and turns the input method off through the platform command path;
// leaving it releases every held key and starts text input afresh.
func (b *Backend) applyRawMode(src eventSource, tag event.Tag) {
	on := b.raw.Load()
	if on == b.gio.rawOn {
		return
	}
	b.gio.rawOn = on
	b.gio.ime = imeState{}
	if on {
		b.execRecorded(src, tag, KeyboardHide{})
		return
	}
	b.releaseHeld()
	if b.gio.focused {
		b.execRecorded(src, tag, KeyboardShow{})
	}
}

// rawKey reports a key in raw mode, and keeps the set of keys held.
func (b *Backend) rawKey(e key.Event) {
	in := KeyInput{Name: string(e.Name), Mods: modsOf(e.Modifiers), Press: e.State == key.Press}
	k, ok := rawKeyOf(in)
	if !ok {
		return
	}
	if b.gio.held == nil {
		b.gio.held = map[rune]tui.KeyEvent{}
	}
	if k.Kind == tui.KeyPress {
		b.gio.held[k.Code] = k
	} else {
		delete(b.gio.held, k.Code)
	}
	b.q.push(k)
}

// releaseHeld reports the release of every key still held. Operating systems send no release for
// a key held as the window loses focus, so a game would see it stuck down without this.
func (b *Backend) releaseHeld() {
	for code, k := range b.gio.held {
		b.q.push(tui.KeyEvent{Kind: tui.KeyRelease, Code: code, Mods: k.Mods})
		delete(b.gio.held, code)
	}
}
