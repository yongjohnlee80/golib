package yaml

import "slices"

// YAML is the parser as a value, configured once by [New]. It satisfies parse.Parser[*Stream] and
// parse.Named, so code that holds parsers of several formats behind golib/parse's interfaces can hold
// this one too. [Parse] remains the direct way to parse with options.
//
// A YAML value is safe for concurrent use: each Parse builds its own configuration from the options.
type YAML struct{ opts []Option }

// New returns a YAML parser configured by opts.
func New(opts ...Option) YAML { return YAML{opts: slices.Clone(opts)} }

// Parse parses src as [Parse] does with the parser's options.
func (y YAML) Parse(src []byte) (*Stream, error) { return Parse(src, y.opts...) }

// FormatName names the format in diagnostics.
func (YAML) FormatName() string { return "yaml" }
