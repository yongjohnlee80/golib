package view

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/yongjohnlee80/golib/parse"
	pview "github.com/yongjohnlee80/golib/parse/view"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
	"github.com/yongjohnlee80/golib/yaml"
)

// Version is the only .view schema version this package reads.
const Version = 1

// Output formats a view's body renders in. The format decides how each printed value is escaped; see
// [View.Format].
const (
	FormatText     = "text"
	FormatMarkdown = "markdown"
	FormatJSON     = "json"
	FormatXML      = "xml"
	FormatHTML     = "html"
)

// ErrInvalid is wrapped by every error that rejects a .view definition: a missing or unterminated
// frontmatter, an unknown or mistyped field, a template that does not parse.
var ErrInvalid = errors.New("view: invalid definition")

// View is a loaded .view definition: its frontmatter fields and its compiled body and export templates.
// A View is immutable once loaded and safe for concurrent use.
type View struct {
	name        string
	source      string
	args        []string
	process     string
	export      string
	destination string
	format      string

	body     executor
	exportTo *template.Template // nil when the view declares no export
}

// positions are the frontmatter fields' positions in the file, for errors.
type positions struct {
	at  map[string]parse.Position
	top parse.Position // the frontmatter's first line, for a field that is missing
}

func (p positions) of(field string) parse.Position {
	if pos, ok := p.at[field]; ok {
		return pos
	}
	return p.top
}

// Load reads and compiles the .view file at filename, which names it in every error position. See
// [New].
func Load(filename string) (*View, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return compileFile(src, pview.WithName(filename))
}

// New compiles a .view definition. golib/parse/view reads its shape: a line holding exactly "---",
// the YAML frontmatter, the next such line, and the body template, which may itself begin with "---"
// (a Markdown document's own frontmatter). This evaluates what it read.
//
// The frontmatter fields:
//
//	version      required, 1
//	name         required
//	source       required; the dialect name of the connection the view runs on ("postgres", "sqlite", "mysql")
//	args         optional list of names, bound in order as the query's parameters ($1, $2, … in postgres)
//	process      required; the query, which returns exactly one column holding one JSON value per row
//	export       optional template for each document's file name, as text/template sees it ({{.entity_id}})
//	destination  optional URI of where exports go (file:///dir, gs://bucket/prefix)
//	format       optional; text, markdown, json, xml or html. Defaults from export's extension, else text
//
// The frontmatter is read under YAML's Core schema, and a multi-line query is a block scalar
// ("process: |"). An unknown field is an error, so a misspelt optional field cannot silently go
// missing. The query is never a template: "{{" in process is refused, because a value spliced into
// SQL text is an injection, and every argument arrives as a bound parameter instead. A body calling
// a function that is neither one of text/template's nor one this package provides is refused here,
// not when the first row renders. Every error wraps [ErrInvalid], and one the parser found also
// unwraps to its *parse/view.Error.
func New(src []byte) (*View, error) { return compileFile(src) }

func compileFile(src []byte, opts ...pview.Option) (*View, error) {
	f, err := pview.Parse(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	v, pos, err := fields(f)
	if err != nil {
		return nil, err
	}
	if v.body, err = compileBody(f, v.format); err != nil {
		return nil, err
	}
	if v.export != "" {
		if v.exportTo, err = compile(v.name+".export", v.export, FormatText); err != nil {
			return nil, fmt.Errorf("%w: %s: export: %w", ErrInvalid, pos.of("export"), err)
		}
	}
	return v, nil
}

// Name is the frontmatter's name.
func (v *View) Name() string { return v.name }

// Source is the dialect name the view's query is written for. [View.Query] refuses a connection of
// another dialect.
func (v *View) Source() string { return v.source }

// Args are the declared argument names, in the order they bind as query parameters.
func (v *View) Args() []string { return append([]string(nil), v.args...) }

// Process is the query text, as declared.
func (v *View) Process() string { return v.process }

// Export is the export file-name template as declared, or "" when the view declares none.
func (v *View) Export() string { return v.export }

// Destination is the declared export destination URI, or "". The caller opens the filesystem it names
// and hands it to [Invocation.Export]; this package does not dial storage itself.
func (v *View) Destination() string { return v.destination }

// Format is the body's output format, which decides how every printed value is escaped:
//
//	text, markdown  printed as is
//	json            escaped as the content of a JSON string: quotes, backslashes and control characters
//	xml             escaped as character data: & < > ' "; attribute values are quoted, as XML requires
//	html            escaped by html/template for the context the value lands in: text, attribute, URL,
//	                script or style; a URL with an active scheme such as javascript: is replaced
//
// The json escaping assumes values are printed inside a JSON string's quotes; a number or boolean
// printed bare is unchanged by it. In text, markdown, json and xml, a value produced by toJson,
// quote or raw is printed as is. In html, raw marks trusted markup and is printed as is; toJson and
// quote are escaped like any other value. Use html, not xml, for a document a browser renders.
func (v *View) Format() string { return v.format }

// fields evaluates the frontmatter under the YAML Core schema and checks each field's presence and
// type. Errors name the field's position in the file.
func fields(f *pview.File) (*View, positions, error) {
	p := positions{at: map[string]parse.Position{}, top: f.Position(f.Front.Start)}
	if f.Meta == nil || f.Meta.Root == nil {
		return nil, p, fmt.Errorf("%w: %s: the frontmatter is empty", ErrInvalid, p.top)
	}
	val, err := yaml.Evaluate(f.Meta, yaml.Core)
	if err != nil {
		return nil, p, fmt.Errorf("%w: %s: frontmatter: %w", ErrInvalid, p.top, err)
	}
	m, ok := val.(yaml.Map)
	if !ok || f.Meta.Root.Kind != pyaml.KindMapping || len(m) != len(f.Meta.Root.Pairs) {
		return nil, p, fmt.Errorf("%w: %s: the frontmatter must be a mapping", ErrInvalid, p.top)
	}

	v := &View{}
	version := int64(0)
	for i, item := range m {
		// The evaluated mapping keeps its entries in source order, as the parsed one does; the
		// i-th parsed key is checked to be this entry's, so a position is never another field's.
		key, _ := item.Key.(string)
		pk := f.Meta.Root.Pairs[i].Key
		if pk.Kind != pyaml.KindScalar || string(pk.Value) != key {
			return nil, p, fmt.Errorf("%w: %s: frontmatter keys must be plain names", ErrInvalid, f.MetaPosition(pk.Span.Start))
		}
		pos := f.MetaPosition(pk.Span.Start)
		p.at[key] = pos
		var err error
		switch key {
		case "version":
			n, ok := item.Value.(int64)
			if !ok {
				err = errors.New("want an integer")
			}
			version = n
		case "name":
			v.name, err = str(item.Value)
		case "source":
			v.source, err = str(item.Value)
		case "process":
			v.process, err = str(item.Value)
		case "export":
			v.export, err = str(item.Value)
		case "destination":
			v.destination, err = str(item.Value)
		case "format":
			v.format, err = str(item.Value)
		case "args":
			v.args, err = names(item.Value)
		default:
			err = errors.New("unknown field")
		}
		if err != nil {
			return nil, p, fmt.Errorf("%w: %s: %s: %w", ErrInvalid, pos, key, err)
		}
	}

	switch {
	case version != Version:
		return nil, p, fmt.Errorf("%w: %s: version: want %d, have %d", ErrInvalid, p.of("version"), Version, version)
	case strings.TrimSpace(v.name) == "":
		return nil, p, fmt.Errorf("%w: %s: name is required", ErrInvalid, p.of("name"))
	case strings.TrimSpace(v.source) == "":
		return nil, p, fmt.Errorf("%w: %s: source is required", ErrInvalid, p.of("source"))
	case strings.TrimSpace(v.process) == "":
		return nil, p, fmt.Errorf("%w: %s: process is required", ErrInvalid, p.of("process"))
	case strings.Contains(v.process, "{{"):
		return nil, p, fmt.Errorf("%w: %s: process is not a template; bind each argument as a query parameter instead of {{…}}", ErrInvalid, p.of("process"))
	}
	if v.destination != "" {
		if u, err := url.Parse(v.destination); err != nil || u.Scheme == "" {
			return nil, p, fmt.Errorf("%w: %s: destination: want a URI with a scheme, such as file:///dir or gs://bucket", ErrInvalid, p.of("destination"))
		}
	}
	if v.format == "" {
		v.format = formatOf(v.export)
	}
	switch v.format {
	case FormatText, FormatMarkdown, FormatJSON, FormatXML, FormatHTML:
	default:
		return nil, p, fmt.Errorf("%w: %s: format: unknown format %q", ErrInvalid, p.of("format"), v.format)
	}
	return v, p, nil
}

// formatOf infers an output format from an export file name's extension.
func formatOf(export string) string {
	switch strings.ToLower(path.Ext(export)) {
	case ".json":
		return FormatJSON
	case ".xml":
		return FormatXML
	case ".html", ".htm":
		return FormatHTML
	case ".md", ".markdown":
		return FormatMarkdown
	}
	return FormatText
}

func str(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", errors.New("want a string")
	}
	return s, nil
}

// names reads the args list: distinct, non-empty strings.
func names(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("want a list of names")
	}
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, errors.New("want a list of names")
		}
		if seen[s] {
			return nil, fmt.Errorf("%q is declared twice", s)
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}
