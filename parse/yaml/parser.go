package yaml

type pstate uint8

const (
	psStreamStart pstate = iota
	psImplicitDocumentStart
	psDocumentStart
	psDocumentContent
	psDocumentEnd
	psBlockNode
	psBlockSequenceFirstEntry
	psBlockSequenceEntry
	psIndentlessSequenceEntry
	psBlockMappingFirstKey
	psBlockMappingKey
	psBlockMappingValue
	psFlowSequenceFirstEntry
	psFlowSequenceEntry
	psFlowSequenceEntryMappingKey
	psFlowSequenceEntryMappingValue
	psFlowSequenceEntryMappingEnd
	psFlowMappingFirstKey
	psFlowMappingKey
	psFlowMappingValue
	psFlowMappingEmptyValue
	psEnd
)

// parser consumes tokens by the productions of spec chapters 6–9 and emits events.
type parser struct {
	s      scanner
	cfg    config
	src    []byte
	enc    Encoding
	state  pstate
	states []pstate
	depth  int

	tags       map[string]string // the current document's tag handles
	directives []Directive
	anchors    map[string]bool // anchors seen so far in the current document
	// endedExplicitly: the previous document ended with "...", so a bare document or directives may
	// follow; otherwise only "---" may start the next one.
	endedExplicitly bool
	yamlDirective   bool
}

func newParser(src []byte, cfg config) (*parser, error) {
	p := &parser{cfg: cfg}
	enc := detectEncoding(src)
	utf, bad := toUTF8(src, enc)
	p.enc = enc
	if bad >= 0 {
		p.src = src[:0]
		return p, &Error{Pos: position(src, lineStarts(src), bad), Msg: "the input is not valid in its encoding"}
	}
	p.src = utf
	if bad := firstUnprintable(utf); bad >= 0 {
		return p, &Error{Pos: position(utf, lineStarts(utf), bad), Msg: "a character YAML does not allow (not c-printable)"}
	}
	p.s = scanner{src: utf}
	return p, nil
}

func (p *parser) stream() *Stream { return &Stream{Source: p.src, Encoding: p.enc} }

func (p *parser) peek() (*token, error) { return p.s.token() }

func (p *parser) errorAt(m mark, msg string) error { return p.s.errorf(m, msg) }

func (p *parser) push(st pstate) { p.states = append(p.states, st) }

func (p *parser) pop() {
	p.state = p.states[len(p.states)-1]
	p.states = p.states[:len(p.states)-1]
}

func span(a, b mark) Span { return Span{a.off, b.off} }

func (p *parser) next() (Event, error) {
	switch p.state {
	case psStreamStart:
		t, err := p.peek()
		if err != nil {
			return Event{}, err
		}
		p.s.consume()
		p.state = psImplicitDocumentStart
		return Event{Kind: EventStreamStart, Span: span(t.start, t.end)}, nil
	case psImplicitDocumentStart:
		return p.documentStart(true)
	case psDocumentStart:
		return p.documentStart(p.endedExplicitly)
	case psDocumentContent:
		t, err := p.peek()
		if err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tVersionDirective, tTagDirective, tReservedDirective, tDocumentStart, tDocumentEnd, tStreamEnd:
			p.pop()
			return emptyScalar(t.start), nil
		}
		return p.node(true, false)
	case psDocumentEnd:
		return p.documentEnd()
	case psBlockNode:
		return p.node(true, false)
	case psBlockSequenceFirstEntry:
		return p.blockSequenceEntry(true)
	case psBlockSequenceEntry:
		return p.blockSequenceEntry(false)
	case psIndentlessSequenceEntry:
		return p.indentlessSequenceEntry()
	case psBlockMappingFirstKey:
		return p.blockMappingKey(true)
	case psBlockMappingKey:
		return p.blockMappingKey(false)
	case psBlockMappingValue:
		return p.blockMappingValue()
	case psFlowSequenceFirstEntry:
		return p.flowSequenceEntry(true)
	case psFlowSequenceEntry:
		return p.flowSequenceEntry(false)
	case psFlowSequenceEntryMappingKey:
		return p.flowSequenceEntryMappingKey()
	case psFlowSequenceEntryMappingValue:
		return p.flowSequenceEntryMappingValue()
	case psFlowSequenceEntryMappingEnd:
		t, err := p.peek()
		if err != nil {
			return Event{}, err
		}
		p.state = psFlowSequenceEntry
		p.depth--
		return Event{Kind: EventMappingEnd, Span: span(t.start, t.start)}, nil
	case psFlowMappingFirstKey:
		return p.flowMappingKey(true)
	case psFlowMappingKey:
		return p.flowMappingKey(false)
	case psFlowMappingValue:
		return p.flowMappingValue(false)
	case psFlowMappingEmptyValue:
		return p.flowMappingValue(true)
	}
	return Event{}, &Error{Msg: "no events after the end of the stream"}
}

func emptyScalar(m mark) Event {
	return Event{Kind: EventScalar, Style: StylePlain, Value: []byte{}, Span: span(m, m)}
}

func (p *parser) documentStart(implicitAllowed bool) (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	// "..." may repeat, and may end nothing
	for t.kind == tDocumentEnd {
		p.s.consume()
		p.endedExplicitly = true
		implicitAllowed = true
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
	}
	p.tags = map[string]string{"!": "!", "!!": "tag:yaml.org,2002:"}
	p.anchors = map[string]bool{}
	p.directives = nil
	p.yamlDirective = false
	switch t.kind {
	case tStreamEnd:
		p.s.consume()
		p.state = psEnd
		return Event{Kind: EventStreamEnd, Span: span(t.start, t.end)}, nil
	case tVersionDirective, tTagDirective, tReservedDirective, tDocumentStart:
		if t.kind != tDocumentStart && !implicitAllowed {
			return Event{}, p.errorAt(t.start, "directives must follow a document end marker (\"...\")")
		}
		start := t.start
		if err := p.processDirectives(); err != nil {
			return Event{}, err
		}
		t, err := p.peek()
		if err != nil {
			return Event{}, err
		}
		if t.kind != tDocumentStart {
			return Event{}, p.errorAt(t.start, "directives must be followed by \"---\"")
		}
		end := t.end
		p.s.consume()
		p.push(psDocumentEnd)
		p.state = psDocumentContent
		return Event{Kind: EventDocumentStart, Explicit: true, Directives: p.directives, Span: span(start, end)}, nil
	}
	if !implicitAllowed {
		return Event{}, p.errorAt(t.start, "a document after one not ended by \"...\" must start with \"---\"")
	}
	p.push(psDocumentEnd)
	p.state = psBlockNode
	return Event{Kind: EventDocumentStart, Span: span(t.start, t.start)}, nil
}

func (p *parser) processDirectives() error {
	for {
		t, err := p.peek()
		if err != nil {
			return err
		}
		d := Directive{Name: string(t.value), Params: t.params, Span: span(t.start, t.end)}
		switch t.kind {
		case tVersionDirective:
			if p.yamlDirective {
				return p.errorAt(t.start, "a document may have one %YAML directive")
			}
			p.yamlDirective = true
			if t.params[0][0] != '1' || t.params[0][1] != '.' {
				return p.errorAt(t.start, "this parser reads YAML 1.x")
			}
		case tTagDirective:
			h := string(t.handle)
			if _, dup := p.tags[h]; dup && p.declared(h) {
				return p.errorAt(t.start, "a %TAG directive repeats a handle")
			}
			p.tags[h] = string(t.prefix)
		case tReservedDirective:
			// reserved for future use: ignored (spec 6.8)
		default:
			return nil
		}
		p.directives = append(p.directives, d)
		p.s.consume()
	}
}

// declared reports whether handle came from a %TAG directive of this document, not a default.
func (p *parser) declared(handle string) bool {
	for _, d := range p.directives {
		if d.Name == "TAG" && len(d.Params) > 0 && d.Params[0] == handle {
			return true
		}
	}
	return false
}

func (p *parser) documentEnd() (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	ev := Event{Kind: EventDocumentEnd, Span: span(t.start, t.start)}
	p.endedExplicitly = false
	if t.kind == tDocumentEnd {
		ev.Explicit = true
		ev.Span = span(t.start, t.end)
		p.endedExplicitly = true
		p.s.consume()
	} else if t.kind != tDocumentStart && t.kind != tStreamEnd {
		return Event{}, p.errorAt(t.start, "a document has one root node: expected the document's end")
	}
	p.state = psDocumentStart
	return ev, nil
}

// node parses a node: its properties, then an alias, a scalar, or a collection's start.
func (p *parser) node(block, indentlessSequence bool) (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind == tAlias {
		name := string(t.value)
		if !p.anchors[name] {
			return Event{}, p.errorAt(t.start, "an alias names an anchor not defined before it (\""+name+"\")")
		}
		p.pop()
		p.s.consume()
		return Event{Kind: EventAlias, Alias: name, Span: span(t.start, t.end)}, nil
	}
	start := t.start
	var anchor, tag string
	for i := 0; i < 2; i++ {
		switch t.kind {
		case tAnchor:
			if anchor != "" {
				return Event{}, p.errorAt(t.start, "a node may have one anchor")
			}
			anchor = string(t.value)
		case tTag:
			if tag != "" {
				return Event{}, p.errorAt(t.start, "a node may have one tag")
			}
			if tag, err = p.resolveTag(t); err != nil {
				return Event{}, err
			}
		default:
			i = 2
			continue
		}
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
	}
	if (t.kind == tAnchor && anchor != "") || (t.kind == tTag && tag != "") {
		return Event{}, p.errorAt(t.start, "a node may have one anchor and one tag")
	}
	if anchor != "" {
		p.anchors[anchor] = true
	}
	ev := Event{Anchor: anchor, Tag: tag, Span: span(start, t.end)}
	switch {
	case t.kind == tAlias:
		return Event{}, p.errorAt(t.start, "an alias cannot have properties")
	case indentlessSequence && t.kind == tBlockEntry:
		p.state = psIndentlessSequenceEntry
		ev.Kind, ev.Style = EventSequenceStart, StyleBlock
		ev.Span = span(start, t.start)
		return ev, p.deeper(t.start)
	case t.kind == tScalar:
		p.pop()
		p.s.consume()
		ev.Kind, ev.Style, ev.Value = EventScalar, t.style, t.value
		return ev, nil
	case t.kind == tFlowSequenceStart:
		p.state = psFlowSequenceFirstEntry
		ev.Kind, ev.Style = EventSequenceStart, StyleFlow
		return ev, p.deeper(t.start)
	case t.kind == tFlowMappingStart:
		p.state = psFlowMappingFirstKey
		ev.Kind, ev.Style = EventMappingStart, StyleFlow
		return ev, p.deeper(t.start)
	case block && t.kind == tBlockSequenceStart:
		p.state = psBlockSequenceFirstEntry
		ev.Kind, ev.Style = EventSequenceStart, StyleBlock
		return ev, p.deeper(t.start)
	case block && t.kind == tBlockMappingStart:
		p.state = psBlockMappingFirstKey
		ev.Kind, ev.Style = EventMappingStart, StyleBlock
		return ev, p.deeper(t.start)
	case anchor != "" || tag != "":
		p.pop()
		ev.Kind, ev.Style, ev.Value = EventScalar, StylePlain, []byte{}
		ev.Span = span(start, t.start)
		return ev, nil
	}
	return Event{}, p.errorAt(t.start, "did not find the expected node content")
}

func (p *parser) deeper(m mark) error {
	p.depth++
	if p.depth > p.cfg.maxDepth {
		return p.errorAt(m, "collections nest more deeply than the parser's bound ("+itoa(p.cfg.maxDepth)+")")
	}
	return nil
}

// resolveTag expands a tag's handle with the document's %TAG directives (or the defaults).
func (p *parser) resolveTag(t *token) (string, error) {
	if t.handle == nil {
		return string(t.suffix), nil // verbatim, or the non-specific "!"
	}
	prefix, ok := p.tags[string(t.handle)]
	if !ok {
		return "", p.errorAt(t.start, "the tag handle "+string(t.handle)+" is not defined")
	}
	return prefix + string(t.suffix), nil
}

func (p *parser) blockSequenceEntry(first bool) (Event, error) {
	if first {
		p.s.consume()
	}
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	switch t.kind {
	case tBlockEntry:
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		if t.kind != tBlockEntry && t.kind != tBlockEnd {
			p.push(psBlockSequenceEntry)
			return p.node(true, false)
		}
		p.state = psBlockSequenceEntry
		return emptyScalar(m), nil
	case tBlockEnd:
		p.pop()
		p.s.consume()
		p.depth--
		return Event{Kind: EventSequenceEnd, Span: span(t.start, t.end)}, nil
	}
	return Event{}, p.errorAt(t.start, "did not find the expected '-' of a block sequence entry")
}

func (p *parser) indentlessSequenceEntry() (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind == tBlockEntry {
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tBlockEntry, tKey, tValue, tBlockEnd:
			p.state = psIndentlessSequenceEntry
			return emptyScalar(m), nil
		}
		p.push(psIndentlessSequenceEntry)
		return p.node(true, false)
	}
	p.pop()
	p.depth--
	return Event{Kind: EventSequenceEnd, Span: span(t.start, t.start)}, nil
}

func (p *parser) blockMappingKey(first bool) (Event, error) {
	if first {
		p.s.consume()
	}
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	switch t.kind {
	case tKey:
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tKey, tValue, tBlockEnd:
			p.state = psBlockMappingValue
			return emptyScalar(m), nil
		}
		p.push(psBlockMappingValue)
		return p.node(true, true)
	case tValue:
		p.state = psBlockMappingValue // an empty key
		return emptyScalar(t.start), nil
	case tBlockEnd:
		p.pop()
		p.s.consume()
		p.depth--
		return Event{Kind: EventMappingEnd, Span: span(t.start, t.end)}, nil
	}
	return Event{}, p.errorAt(t.start, "did not find the expected key of a block mapping")
}

func (p *parser) blockMappingValue() (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind == tValue {
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tKey, tValue, tBlockEnd:
			p.state = psBlockMappingKey
			return emptyScalar(m), nil
		}
		p.push(psBlockMappingKey)
		return p.node(true, true)
	}
	p.state = psBlockMappingKey
	return emptyScalar(t.start), nil
}

func (p *parser) flowSequenceEntry(first bool) (Event, error) {
	if first {
		p.s.consume()
	}
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind != tFlowSequenceEnd {
		if !first {
			if t.kind != tFlowEntry {
				return Event{}, p.errorAt(t.start, "did not find the expected ',' or ']' of a flow sequence")
			}
			p.s.consume()
			if t, err = p.peek(); err != nil {
				return Event{}, err
			}
		}
		switch t.kind {
		case tKey, tValue:
			// a single-pair mapping: [key: value], [? key : value], [: value]
			p.state = psFlowSequenceEntryMappingKey
			if t.kind == tKey {
				p.s.consume()
			}
			ev := Event{Kind: EventMappingStart, Style: StyleFlow, Span: span(t.start, t.start)}
			return ev, p.deeper(t.start)
		case tFlowSequenceEnd:
		default:
			p.push(psFlowSequenceEntry)
			return p.node(false, false)
		}
	}
	p.pop()
	p.s.consume()
	p.depth--
	return Event{Kind: EventSequenceEnd, Span: span(t.start, t.end)}, nil
}

func (p *parser) flowSequenceEntryMappingKey() (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	switch t.kind {
	case tValue, tFlowEntry, tFlowSequenceEnd:
		p.state = psFlowSequenceEntryMappingValue
		return emptyScalar(t.start), nil
	}
	p.push(psFlowSequenceEntryMappingValue)
	return p.node(false, false)
}

func (p *parser) flowSequenceEntryMappingValue() (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind == tValue {
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tFlowEntry, tFlowSequenceEnd:
			p.state = psFlowSequenceEntryMappingEnd
			return emptyScalar(m), nil
		}
		p.push(psFlowSequenceEntryMappingEnd)
		return p.node(false, false)
	}
	p.state = psFlowSequenceEntryMappingEnd
	return emptyScalar(t.start), nil
}

func (p *parser) flowMappingKey(first bool) (Event, error) {
	if first {
		p.s.consume()
	}
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if t.kind != tFlowMappingEnd {
		if !first {
			if t.kind != tFlowEntry {
				return Event{}, p.errorAt(t.start, "did not find the expected ',' or '}' of a flow mapping")
			}
			p.s.consume()
			if t, err = p.peek(); err != nil {
				return Event{}, err
			}
		}
		switch t.kind {
		case tKey:
			p.s.consume()
			if t, err = p.peek(); err != nil {
				return Event{}, err
			}
			switch t.kind {
			case tValue, tFlowEntry, tFlowMappingEnd:
				p.state = psFlowMappingValue
				return emptyScalar(t.start), nil
			}
			p.push(psFlowMappingValue)
			return p.node(false, false)
		case tValue:
			p.state = psFlowMappingValue // an empty key
			return emptyScalar(t.start), nil
		case tFlowMappingEnd:
		default:
			p.push(psFlowMappingEmptyValue)
			return p.node(false, false)
		}
	}
	p.pop()
	p.s.consume()
	p.depth--
	return Event{Kind: EventMappingEnd, Span: span(t.start, t.end)}, nil
}

func (p *parser) flowMappingValue(empty bool) (Event, error) {
	t, err := p.peek()
	if err != nil {
		return Event{}, err
	}
	if empty {
		p.state = psFlowMappingKey
		return emptyScalar(t.start), nil
	}
	if t.kind == tValue {
		m := t.end
		p.s.consume()
		if t, err = p.peek(); err != nil {
			return Event{}, err
		}
		switch t.kind {
		case tFlowEntry, tFlowMappingEnd:
			p.state = psFlowMappingKey
			return emptyScalar(m), nil
		}
		p.push(psFlowMappingKey)
		return p.node(false, false)
	}
	p.state = psFlowMappingKey
	return emptyScalar(t.start), nil
}
