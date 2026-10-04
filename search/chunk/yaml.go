package chunk

import (
	"fmt"
	"path"
	"strings"

	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/yaml"
)

// A YAML document is chunked by its top-level entries: each entry of a top-level
// mapping (or item of a top-level sequence) is one unit, its key in the breadcrumb, its text the
// entry flattened to one "key > path: value" line per scalar, so a search finds a value by the keys
// above it as well as by itself. The unit's span is the entry's source. An entry over the section
// limit is split along its lines, each part spanning the values it holds. A stream of several
// documents does this per document. Comments are not indexed: they never change what a value is.
// YAML that does not parse keeps its raw text searchable, with the parse error recorded.

// yamlLine is one flattened scalar: its line of text and the source span of its value.
type yamlLine struct {
	text       string
	start, end int
}

// YAML cuts a YAML document into chunks of at most limit estimated tokens (DefaultTokens when 0),
// one or more per top-level entry, as described above, and reads its title: a top-level "title"
// scalar, else the file name of filePath. YAML that does not parse or evaluate is chunked as plain
// text, with the error in FrontmatterErr.
func YAML(src []byte, filePath string, limit int) (Meta, []search.Chunk) {
	meta := Meta{Title: strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))}
	stream, err := pyaml.Parse(src)
	if err == nil {
		for _, document := range stream.Docs {
			if _, err = yaml.Evaluate(document, yaml.Core); err != nil {
				break
			}
		}
	}
	if err != nil {
		meta.FrontmatterErr = err.Error()
		return meta, Text(src, meta.Title, limit)
	}
	if len(stream.Docs) == 1 && stream.Docs[0].Root != nil && stream.Docs[0].Root.Kind == pyaml.KindMapping {
		for _, pair := range stream.Docs[0].Root.Pairs {
			if pair.Key.Kind == pyaml.KindScalar && string(pair.Key.Value) == "title" && pair.Value.Kind == pyaml.KindScalar {
				if title := strings.TrimSpace(string(pair.Value.Value)); title != "" {
					meta.Title = title
				}
				break
			}
		}
	}
	if limit <= 0 {
		limit = maxTokens
	}
	var chunks []search.Chunk
	unit := func(keyPath string, n *pyaml.Node, start, end int) {
		var lines []yamlLine
		flattenYAML(n, keyPath, &lines)
		if len(lines) == 0 {
			return
		}
		crumb := boundedCrumb(meta.Title, limit)
		if keyPath != "" {
			crumb = boundedCrumb(meta.Title+" > "+keyPath, limit)
		}
		chunks = append(chunks, packYAML(src, crumb, lines, start, end, limit, len(chunks))...)
	}
	for index, document := range stream.Docs {
		prefix := ""
		if len(stream.Docs) > 1 {
			prefix = fmt.Sprintf("document %d", index+1)
		}
		root := document.Root
		switch {
		case root == nil:
		case root.Kind == pyaml.KindMapping:
			for _, pair := range root.Pairs {
				unit(joinKey(prefix, keyOf(pair.Key)), pair.Value, pair.Key.Span.Start, max(pair.Key.Span.End, pair.Value.Span.End))
			}
		case root.Kind == pyaml.KindSequence:
			for i, item := range root.Items {
				unit(joinKey(prefix, fmt.Sprintf("[%d]", i)), item, item.Span.Start, item.Span.End)
			}
		default:
			unit(prefix, root, root.Span.Start, root.Span.End)
		}
	}
	if len(chunks) == 0 {
		chunks = append(chunks, newChunk(0, boundedCrumb(meta.Title, limit), "", 0, 0))
	}
	return meta, chunks
}

// flattenYAML appends one line per scalar under n, each "key path: value" (the value alone at a
// unit's root).
func flattenYAML(n *pyaml.Node, keyPath string, out *[]yamlLine) {
	if n == nil {
		return
	}
	switch n.Kind {
	case pyaml.KindMapping:
		for _, pair := range n.Pairs {
			flattenYAML(pair.Value, joinKey(keyPath, keyOf(pair.Key)), out)
		}
	case pyaml.KindSequence:
		for i, item := range n.Items {
			flattenYAML(item, joinKey(keyPath, fmt.Sprintf("[%d]", i)), out)
		}
	case pyaml.KindScalar, pyaml.KindAlias:
		value := strings.TrimSpace(string(n.Value))
		if n.Kind == pyaml.KindAlias {
			value = "*" + n.Alias
		}
		if value == "" {
			return
		}
		text := value
		if keyPath != "" {
			text = keyPath + ": " + value
		}
		*out = append(*out, yamlLine{text: text, start: n.Span.Start, end: n.Span.End})
	}
}

// packYAML makes a unit's chunks: one, spanning the whole entry, when its lines fit the limit;
// else parts of whole lines, each spanning its own values. A single line over the limit is split
// as plain text over its value's source.
func packYAML(src []byte, crumb string, lines []yamlLine, start, end, limit, base int) []search.Chunk {
	var body []string
	for _, l := range lines {
		body = append(body, l.text)
	}
	whole := strings.Join(body, "\n")
	if tokensOf([]byte(crumb+"\n"+whole), 0, len(crumb)+1+len(whole)) <= limit {
		return []search.Chunk{newChunk(base, crumb, whole, start, end)}
	}
	var out []search.Chunk
	var part []yamlLine
	flush := func() {
		if len(part) == 0 {
			return
		}
		texts := make([]string, len(part))
		for i, l := range part {
			texts[i] = l.text
		}
		out = append(out, newChunk(base+len(out), crumb, strings.Join(texts, "\n"), part[0].start, part[len(part)-1].end))
		part = nil
	}
	fits := func(ls []yamlLine) bool {
		var b strings.Builder
		b.WriteString(crumb)
		for _, l := range ls {
			b.WriteString("\n" + l.text)
		}
		return tokensOf([]byte(b.String()), 0, b.Len()) <= limit
	}
	for _, l := range lines {
		if !fits([]yamlLine{l}) {
			flush()
			for _, p := range Text(src[l.start:l.end], crumb, limit) {
				out = append(out, newChunk(base+len(out), crumb, p.Body, l.start+p.ByteStart, l.start+p.ByteEnd))
			}
			continue
		}
		if !fits(append(append([]yamlLine(nil), part...), l)) {
			flush()
		}
		part = append(part, l)
	}
	flush()
	return out
}

func keyOf(n *pyaml.Node) string {
	if key := strings.TrimSpace(string(n.Value)); key != "" {
		return key
	}
	return "key"
}

func joinKey(prefix, key string) string {
	switch {
	case prefix == "":
		return key
	case key == "":
		return prefix
	}
	return prefix + " > " + key
}
