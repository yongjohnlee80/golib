package view

import "slices"

// View is the parser as a value, configured once by [New]. It satisfies parse.Parser[*File] and
// parse.Named, so code that holds parsers of several formats behind golib/parse's interfaces can hold
// this one too. [Parse] remains the direct way to parse with options.
//
// A View value is safe for concurrent use: each Parse builds its own configuration from the options.
type View struct{ opts []Option }

// New returns a .view parser configured by opts.
func New(opts ...Option) View { return View{opts: slices.Clone(opts)} }

// Parse parses src as [Parse] does with the parser's options.
func (v View) Parse(src []byte) (*File, error) { return Parse(src, v.opts...) }

// FormatName names the format in diagnostics.
func (View) FormatName() string { return "view" }
