// Package msgpack is a zero-dependency MessagePack value codec (server). It
// encodes and decodes the full MessagePack family over a fixed Go
// vocabulary: nil, bool, int64/uint64 (all wire widths), float64 (and
// float32 on the wire), string, []byte, []any, map[string]any (string keys
// only, both directions), and Ext (application extension types — e.g.
// Neovim's Buffer/Window/Tabpage handles).
//
// Decoding treats every input as attacker-adjacent: declared lengths and
// counts are validated against Limits before allocation, collection
// preallocation is capped so a forged giant header cannot allocate memory it
// never supplies, nesting depth is bounded, and no input can panic the
// decoder — enforced by fuzz tests.
//
// Deliberate v1 restrictions (loud, typed errors — never silent): map keys
// must be strings; the msgpack timestamp extension (type -1) is not
// interpreted and passes through as Ext.
package msgpack
