package gui

import "github.com/yongjohnlee80/gio/unit"

// Options is what a list of options resolves to, read before any window opens: a consumer checks
// the window it asks for (WithDecorations(false), a title, a size, the zero-copy renderer) as a
// value, with no window and no source reading. It is the configuration NewBackend starts from.
type Options struct{ c config }

// ResolveOptions applies opts to the defaults, in order, as NewBackend does: a nil option is no
// option, and a later option overrides an earlier one.
func ResolveOptions(opts ...Option) Options { return Options{c: resolve(opts)} }

// resolve is the defaults with opts applied: NewBackend's configuration, and ResolveOptions'.
func resolve(opts []Option) config {
	cfg := defaultConfig()
	for _, o := range opts {
		if o != nil { // a nil option is no option
			o(&cfg)
		}
	}
	return cfg
}

// Decorations says whether the system draws the window's decorations (title bar, edges), and
// whether an option chose that: when set is false, the platform decides, and on means nothing.
func (o Options) Decorations() (on, set bool) { return o.c.decorated, o.c.decoration }

// Title is the window's title.
func (o Options) Title() string { return o.c.title }

// Size is the window's initial size, in device-independent pixels.
func (o Options) Size() (w, h unit.Dp) { return o.c.width, o.c.height }

// ZeroCopyFrames says whether the window asks for the renderer that draws frames without a copy
// (WithZeroCopyFrames).
func (o Options) ZeroCopyFrames() bool { return o.c.zeroCopy }
