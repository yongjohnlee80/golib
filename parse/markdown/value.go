package markdown

import "slices"

// Markdown is the parser as a value, configured once by [New]. It satisfies parse.Parser[*Document]
// and parse.Named, so code that holds parsers of several formats behind golib/parse's interfaces can
// hold this one too. [Parse] remains the direct way to parse with options.
//
// A Markdown value is safe for concurrent use: each Parse builds its own configuration from the
// options.
type Markdown struct{ opts []Option }

// New returns a Markdown parser configured by opts.
func New(opts ...Option) Markdown { return Markdown{opts: slices.Clone(opts)} }

// Parse parses src as [Parse] does with the parser's options. CommonMark defines a reading for every
// input, so the error is always nil; it is there to satisfy parse.Parser.
func (m Markdown) Parse(src []byte) (*Document, error) { return Parse(src, m.opts...), nil }

// FormatName names the format in diagnostics.
func (Markdown) FormatName() string { return "markdown" }
