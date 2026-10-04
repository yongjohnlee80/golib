package view

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"strings"
	"text/template"
	"text/template/parse"

	pview "github.com/yongjohnlee80/golib/parse/view"
)

// Raw is text a template prints without the format's escaping. toJson, quote and raw return it; a
// value is only ever Raw because the template author asked for it by name.
type Raw string

// escapeFunc is the name of the function appended to every printing action; no author would name a
// function this way by accident, and it is not documented as one to call.
const escapeFunc = "_view_escape"

// funcs are the helpers every body and export template may call.
func funcs(format string) template.FuncMap {
	return template.FuncMap{
		escapeFunc: func(v any) (string, error) { return escape(format, v) },
		"toJson": func(v any) (Raw, error) {
			b, err := marshal(v)
			return Raw(b), err
		},
		"quote": func(v any) (Raw, error) {
			b, err := marshal(text(v))
			return Raw(b), err
		},
		"raw":   func(v any) Raw { return Raw(text(v)) },
		"trim":  func(v any) string { return strings.TrimSpace(text(v)) },
		"upper": func(v any) string { return strings.ToUpper(text(v)) },
		"lower": func(v any) string { return strings.ToLower(text(v)) },
		// default returns fallback when v is missing, null, or an empty string, array or object;
		// the argument order reads as a pipeline: {{.genre | default "unknown"}}.
		"default": func(fallback, v any) any {
			if empty(v) {
				return fallback
			}
			return v
		},
	}
}

// compile parses a template given as text, the export file name, and protects what it prints; see
// [compileBody].
func compile(name, src, format string) (*template.Template, error) {
	t, err := template.New(name).Funcs(funcs(format)).Parse(src)
	if err != nil {
		return nil, err
	}
	for _, tt := range t.Templates() {
		guard(tt.Tree, tt.Tree.Root)
	}
	return t, nil
}

// compileBody builds the body's template from the trees golib/parse/view parsed, which name no
// function, and checks every function they call before any row renders.
//
// Every value the body prints is routed through the format's escaping by adding it to the parse
// tree rather than leaving it to each author: a body that prints a lyric into a JSON string must not
// be able to forget it, and one that forgot it would produce invalid JSON only for the rows whose
// text happens to hold a quote. html/template protects its output the same way, by appending
// escapers to each action's pipeline.
func compileBody(f *pview.File, format string) (*template.Template, error) {
	fm := funcs(format)
	t := template.New(pview.BodyTemplate).Funcs(fm)
	known := map[string]bool{}
	for name, tree := range f.Templates {
		var bad *parse.IdentifierNode
		calls(tree.Root, func(id *parse.IdentifierNode) {
			if bad == nil && !defined(fm, known, id.Ident) {
				bad = id
			}
		})
		if bad != nil {
			return nil, fmt.Errorf("%w: %s: function %q is not defined", ErrInvalid, f.TemplatePosition(bad.Pos), bad.Ident)
		}
		guard(tree, tree.Root)
		if _, err := t.AddParseTree(name, tree); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, f.Position(f.Body.Start), err)
		}
	}
	return t.Lookup(pview.BodyTemplate), nil
}

// defined reports whether a template may call the function name: one of fm's or one text/template
// provides. text/template exports no list of its own functions, so the answer comes from asking it to
// parse a call to name, which fails only when no such function exists; known caches each answer.
func defined(fm template.FuncMap, known map[string]bool, name string) bool {
	ok, seen := known[name]
	if !seen {
		_, err := template.New("").Funcs(fm).Parse("{{" + name + "}}")
		ok = err == nil
		known[name] = ok
	}
	return ok
}

// calls visits every function name a template tree calls.
func calls(n parse.Node, visit func(*parse.IdentifierNode)) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			calls(c, visit)
		}
	case *parse.ActionNode:
		calls(n.Pipe, visit)
	case *parse.IfNode:
		calls(&n.BranchNode, visit)
	case *parse.RangeNode:
		calls(&n.BranchNode, visit)
	case *parse.WithNode:
		calls(&n.BranchNode, visit)
	case *parse.BranchNode:
		calls(n.Pipe, visit)
		calls(n.List, visit)
		calls(n.ElseList, visit)
	case *parse.TemplateNode:
		calls(n.Pipe, visit)
	case *parse.PipeNode:
		if n == nil {
			return
		}
		for _, c := range n.Cmds {
			calls(c, visit)
		}
	case *parse.CommandNode:
		for _, a := range n.Args {
			calls(a, visit)
		}
	case *parse.ChainNode:
		calls(n.Node, visit)
	case *parse.IdentifierNode:
		visit(n)
	}
}

// guard appends the escape call to every action that prints. An action that declares or assigns a
// variable prints nothing and is left alone, as are the pipelines of if, range and with, which
// choose rather than print.
func guard(tree *parse.Tree, n parse.Node) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			guard(tree, c)
		}
	case *parse.ActionNode:
		if len(n.Pipe.Decl) > 0 {
			return
		}
		id := parse.NewIdentifier(escapeFunc).SetTree(tree).SetPos(n.Pos)
		n.Pipe.Cmds = append(n.Pipe.Cmds, &parse.CommandNode{NodeType: parse.NodeCommand, Pos: n.Pos, Args: []parse.Node{id}})
	case *parse.IfNode:
		guard(tree, n.List)
		guard(tree, n.ElseList)
	case *parse.RangeNode:
		guard(tree, n.List)
		guard(tree, n.ElseList)
	case *parse.WithNode:
		guard(tree, n.List)
		guard(tree, n.ElseList)
	}
}

// escape renders one printed value for format. A missing key or a JSON null prints as nothing rather
// than text/template's "<no value>", since an absent database value has no text.
func escape(format string, v any) (string, error) {
	if r, ok := v.(Raw); ok {
		return string(r), nil
	}
	s := text(v)
	switch format {
	case FormatJSON:
		b, err := marshal(s)
		if err != nil {
			return "", err
		}
		return string(b[1 : len(b)-1]), nil
	case FormatXML, FormatHTML:
		return html.EscapeString(s), nil
	}
	return s, nil
}

// text is the printed form of one decoded JSON value: nothing for null, the source digits for a
// number, and compact JSON for an array or object, which reads better than Go's map[...] syntax.
func text(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case Raw:
		return string(v)
	case map[string]any, []any:
		b, err := marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
	return fmt.Sprint(v)
}

// empty reports whether v counts as absent for default.
func empty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case Raw:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

// marshal encodes v as JSON without the HTML escaping encoding/json applies by default: a document
// for an embedding model or a file wants "<" and "&" as written, not as a \u escape.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// decode reads one row's JSON as template data. Numbers stay json.Number, so an id or an amount
// prints with the digits the database sent rather than through a float64.
func decode(x []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(x))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("view: row is not JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("view: row holds more than one JSON value")
	}
	return data, nil
}

// Execute renders the body for one row, x being the row's JSON as [Rows.Next] returns it, and
// writes the document to w.
func (v *View) Execute(w io.Writer, x []byte) error {
	data, err := decode(x)
	if err != nil {
		return err
	}
	return v.body.Execute(w, data)
}

// Parse renders the body for one row and returns the document.
func (v *View) Parse(x []byte) (string, error) {
	var b strings.Builder
	if err := v.Execute(&b, x); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Read renders the body for one row as a stream, for a consumer that reads rather than holds the
// document: an embedding request body, an upload. A row that is not JSON fails here; an error while
// rendering is returned by the stream's Read.
//
// The document is written by a goroutine into a pipe, so it is never held whole. The goroutine ends
// when the document is written or when the caller closes the stream, whichever is first; a caller
// that stops reading early must Close, or the goroutine waits on its next write.
func (v *View) Read(x []byte) (io.ReadCloser, error) {
	data, err := decode(x)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(v.body.Execute(pw, data))
	}()
	return pr, nil
}

// exportName renders the export template for one row.
func (v *View) exportName(data any) (string, error) {
	var b strings.Builder
	if err := v.exportTo.Execute(&b, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}
