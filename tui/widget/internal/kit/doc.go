// Package kit is the machinery tui/widget's widgets share that needs none of their public types:
// the frame and row painters, text fitting (truncation, padding, wrapping), the count grammar, Vim's
// word motions, a pointer drag's lifecycle, the press pairer, the repeating ticker, the key-command
// registry (Cmd, Declared) and the small parts of the tab strip.
//
// It is internal: only tui/widget and the packages under it import it, so none of its names is
// public API.
package kit
