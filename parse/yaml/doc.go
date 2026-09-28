// Package yaml parses YAML 1.2 into the specification's event stream and a tree of nodes, and
// resolves nothing: a plain scalar is its text and its style, and a tag is kept as written with its
// %TAG handle expanded. Deciding what a node means is a schema's job; golib/yaml evaluates the tree.
//
//	st, err := yaml.Parse(src) // an ill-formed stream is an *Error with a position
//	for ev, err := range yaml.Events(src) {
//		_, _ = ev, err // the same parse, as events
//	}
//
// Aliases are bound to their anchors' nodes (Node.Target) but never expanded, and nesting is bounded
// by MaxDepth. Input in UTF-16 or UTF-32 is transcoded to UTF-8 first; spans and positions index
// Stream.Source.
//
// https://yaml.org/spec/1.2.2/
package yaml
