package code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
)

// Go chunks Go source by declaration: the file header (build tags, package doc, package clause and
// imports), each top-level function or method, each type spec and each const or var group, with
// anything else that is not whitespace in glue chunks between them. A unit's span runs from its doc
// comment to its end, a trailing comment on its last line included, so lexical search and an
// editor's jump see exactly the file. A file that does not parse is chunked as plain text, so a
// broken file stays searchable.
type Go struct{}

// goVersion folds in the text chunker's version: a change to the fallback re-chunks Go files too.
const goVersion = "code-go-2+text-" + chunk.Version

func (Go) Version() string { return goVersion }

func (Go) Chunk(d search.Doc) ([]search.Chunk, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, d.Path, d.Text, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		title := d.Title
		if title == "" {
			title = d.Path
		}
		return chunk.Text(d.Text, title, d.Tokens), nil
	}
	g := goFile{src: d.Text, fset: fset, file: f}
	dir := path.Dir(d.Path)
	if dir == "." || dir == "/" || dir == "" {
		dir = f.Name.Name
	}
	g.dir = dir
	units := g.units()
	return assemble(d.Text, units, dir, dir+" > (glue)", d.Tokens), nil
}

type goFile struct {
	src  []byte
	fset *token.FileSet
	file *ast.File
	dir  string
}

func (g goFile) off(p token.Pos) int { return g.fset.Position(p).Offset }

// lineEnd extends end past a comment that starts on the same line, so a trailing comment stays
// with the declaration it annotates.
func (g goFile) lineEnd(end int) int {
	line := g.fset.Position(g.fset.File(g.file.Pos()).Pos(min(end, len(g.src)))).Line
	for _, cg := range g.file.Comments {
		s := g.off(cg.Pos())
		if s >= end && g.fset.Position(cg.Pos()).Line == line {
			return max(end, g.off(cg.End()))
		}
	}
	return end
}

func (g goFile) units() []unit {
	var units []unit
	// The header runs from the first byte through the last import: build tags, the package doc and
	// the clause stay with the imports.
	headerEnd := g.off(g.file.Name.End())
	for _, d := range g.file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			headerEnd = max(headerEnd, g.off(gd.End()))
		}
	}
	headerEnd = g.lineEnd(headerEnd)
	units = append(units, unit{start: 0, end: headerEnd, crumb: g.dir})
	for _, d := range g.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			units = append(units, g.funcUnit(d))
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			units = append(units, g.genUnits(d)...)
		}
		// A BadDecl is text the parser skipped: it falls to a glue chunk.
	}
	sort.SliceStable(units, func(i, j int) bool { return units[i].start < units[j].start })
	return units
}

func (g goFile) span(doc *ast.CommentGroup, node ast.Node) (int, int) {
	start := g.off(node.Pos())
	if doc != nil {
		start = min(start, g.off(doc.Pos()))
	}
	return start, g.lineEnd(g.off(node.End()))
}

func (g goFile) funcUnit(fd *ast.FuncDecl) unit {
	start, end := g.span(fd.Doc, fd)
	crumb := g.dir
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		crumb += " > " + recvName(fd.Recv.List[0].Type)
	}
	crumb += " > " + fd.Name.Name
	sigEnd := g.off(fd.End())
	if fd.Body != nil {
		sigEnd = g.off(fd.Body.Lbrace)
	}
	sig := strings.TrimSpace(string(g.src[g.off(fd.Pos()):sigEnd]))
	u := unit{
		start: start, end: end, crumb: crumb, sig: oneLine(sig), tail: sig,
		embed: joinNonEmpty(crumb, docText(fd.Doc), sig),
	}
	if fd.Body != nil && len(fd.Body.List) > 1 {
		for i := 1; i < len(fd.Body.List); i++ {
			prev := g.lineEnd(g.off(fd.Body.List[i-1].End()))
			u.cuts = append(u.cuts, nextNonSpace(g.src, prev))
		}
		u.fragEmbed = func(s, e int) string { return joinNonEmpty(oneLine(sig), g.summary(fd.Body, s, e)) }
	}
	return u
}

// summary is what a later fragment of a split function embeds besides the signature: its comments
// and the identifiers it declares and calls, so control flow and error boilerplate stay out of the
// vector while what the code does stays in.
func (g goFile) summary(body *ast.BlockStmt, s, e int) string {
	var comments []string
	for _, cg := range g.file.Comments {
		if cs := g.off(cg.Pos()); cs >= s && cs < e {
			comments = append(comments, strings.TrimSpace(cg.Text()))
		}
	}
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n != "" && n != "_" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, st := range body.List {
		if ss := g.off(st.Pos()); ss < s || ss >= e {
			continue
		}
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE {
					for _, l := range n.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							add(id.Name)
						}
					}
				}
			case *ast.ValueSpec:
				for _, id := range n.Names {
					add(id.Name)
				}
			case *ast.CallExpr:
				add(callName(n.Fun))
			}
			return true
		})
	}
	return joinNonEmpty(strings.Join(comments, "\n"), strings.Join(names, " "))
}

func (g goFile) genUnits(gd *ast.GenDecl) []unit {
	start, end := g.span(gd.Doc, gd)
	if gd.Tok != token.TYPE {
		crumb := g.dir + " > " + gd.Tok.String() + " " + specNames(gd)
		return []unit{{start: start, end: end, crumb: crumb,
			embed: joinNonEmpty(crumb, docText(gd.Doc), g.valueDecl(gd))}}
	}
	if len(gd.Specs) <= 1 {
		crumb := g.dir
		if len(gd.Specs) == 1 {
			crumb += " > " + typeCrumb(gd.Specs[0].(*ast.TypeSpec))
		}
		return []unit{{start: start, end: end, crumb: crumb,
			embed: joinNonEmpty(crumb, docText(gd.Doc), string(g.src[g.off(gd.Pos()):g.off(gd.End())]))}}
	}
	// A grouped type declaration: a unit per spec, the first taking "type (" and the group's doc,
	// the last taking ")", so the group's span is partitioned with no glue of its own.
	var out []unit
	for i, sp := range gd.Specs {
		ts := sp.(*ast.TypeSpec)
		s, e := g.span(ts.Doc, ts)
		if i == 0 {
			s = start
		}
		if i == len(gd.Specs)-1 {
			e = end
		}
		crumb := g.dir + " > " + typeCrumb(ts)
		doc := docText(ts.Doc)
		if i == 0 {
			doc = joinNonEmpty(docText(gd.Doc), doc)
		}
		out = append(out, unit{start: s, end: e, crumb: crumb,
			embed: joinNonEmpty(crumb, doc, string(g.src[g.off(ts.Pos()):g.off(ts.End())]))})
	}
	return out
}

// valueDecl is what a const or var declaration embeds: each spec's doc, names, type and values,
// with a value's literal reduced to what says what it is. A table's rows or a function literal's
// body would make the embedding the size of the code, and say nothing a few of them do not.
func (g goFile) valueDecl(gd *ast.GenDecl) string {
	var specs []string
	for _, sp := range gd.Specs {
		vs, ok := sp.(*ast.ValueSpec)
		if !ok {
			continue
		}
		var names []string
		for _, n := range vs.Names {
			names = append(names, n.Name)
		}
		line := strings.Join(names, ", ")
		if vs.Type != nil {
			line += " " + oneLine(g.text(vs.Type))
		}
		if len(vs.Values) > 0 {
			var vals []string
			for _, v := range vs.Values {
				vals = append(vals, g.valueOf(v))
			}
			line += " = " + strings.Join(vals, ", ")
		}
		specs = append(specs, joinNonEmpty(docText(vs.Doc), line))
	}
	kw := gd.Tok.String()
	if !gd.Lparen.IsValid() && len(specs) == 1 {
		return kw + " " + specs[0]
	}
	return kw + " (\n" + strings.Join(specs, "\n") + "\n)"
}

// The most of a composite literal's elements, and of a value's bytes, a declaration embeds.
const (
	previewElems = 3
	maxValue     = 80
)

// valueOf is a value as a declaration embeds it: a composite literal as its type and first
// elements, a function literal as its signature, anything else as written, cut when long.
func (g goFile) valueOf(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.CompositeLit:
		text := oneLine(g.text(v))
		if len(v.Elts) <= previewElems && len(text) <= maxValue {
			return text
		}
		var elts []string
		for _, el := range v.Elts[:min(previewElems, len(v.Elts))] {
			elts = append(elts, cut(oneLine(g.text(el)), maxValue))
		}
		if len(v.Elts) > previewElems {
			elts = append(elts, elided)
		}
		typ := ""
		if v.Type != nil {
			typ = oneLine(g.text(v.Type))
		}
		return typ + "{" + strings.Join(elts, ", ") + "}"
	case *ast.FuncLit:
		return oneLine(g.text(v.Type)) + " { " + elided + " }"
	}
	return cut(oneLine(g.text(e)), maxValue)
}

func (g goFile) text(n ast.Node) string { return string(g.src[g.off(n.Pos()):g.off(n.End())]) }

// cut is s up to n bytes, at a rune boundary, with an elision mark when shortened.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + elided
}

func typeCrumb(ts *ast.TypeSpec) string {
	kind := ""
	switch ts.Type.(type) {
	case *ast.StructType:
		kind = " (struct)"
	case *ast.InterfaceType:
		kind = " (interface)"
	}
	return ts.Name.Name + kind
}

func specNames(gd *ast.GenDecl) string {
	var names []string
	for _, sp := range gd.Specs {
		if vs, ok := sp.(*ast.ValueSpec); ok {
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) > 3 {
		names = append(names[:3], "…")
	}
	return strings.Join(names, ", ")
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

func callName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	case *ast.IndexExpr:
		return callName(f.X)
	}
	return ""
}

func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text())
}

func nextNonSpace(src []byte, i int) int {
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	return i
}
