package view

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	tparse "text/template/parse"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/yaml"
)

// BodyTemplate is the name the body's own template has in [File.Templates]. Templates the body
// defines with {{define}} sit beside it under their own names.
const BodyTemplate = "body"

// Span is a half-open byte range [Start, End) of [File.Source].
type Span struct{ Start, End int }

// File is a parsed .view file. Every span indexes Source.
type File struct {
	// Source is the file as given.
	Source []byte
	// Name is the file name given with [WithName], carried into every position.
	Name string

	// Open and Close are the frontmatter's delimiter lines, each exactly "---" with an optional
	// trailing "\r", without the line feed that ends them.
	Open, Close Span
	// Front is the frontmatter between the delimiter lines.
	Front Span
	// Meta is the frontmatter's YAML document, nil when the frontmatter is empty. Its node spans and
	// positions index the frontmatter, not Source; [File.MetaPosition] translates them.
	Meta *yaml.Document
	// Body is everything after the closing delimiter line, to the end of the file.
	Body Span
	// Templates are the body's template trees: the body itself under [BodyTemplate], and each
	// template it defines under that template's name. Function names are not checked, since which
	// functions exist is decided by whoever executes the body; [tparse.Tree.Mode] says so. A node's
	// Pos translates to a file position with [File.TemplatePosition].
	Templates map[string]*tparse.Tree

	pad   int   // newlines the body was parsed behind, so the template's line numbers are the file's
	lines []int // offsets where each line of Source starts
}

// Error is an ill-formed .view file: where, and what is wrong there.
type Error struct {
	Pos parse.Position
	Msg string
	Err error // the YAML or template error behind Msg, when there is one
	// Incomplete says the file ended in the middle of a construct, so more text appended to it could
	// make it valid: an opening "---" not yet typed in full, a frontmatter with no closing line yet,
	// or a body whose actions, comments, strings or blocks are still open. Otherwise the file is
	// wrong where it stands. It selects which of golib/parse's two identities the error answers:
	// ErrUnterminated or ErrSyntax.
	Incomplete bool
}

func (e *Error) Error() string { return "view: " + e.Pos.String() + ": " + e.Msg }

// Unwrap returns the YAML or template error the file failed with, or nil.
func (e *Error) Unwrap() error { return e.Err }

// As also answers for golib/parse's shared syntax error, with the position in the .view file, so a
// caller handling several formats reads where any of them failed the same way, with a
// parse.SyntaxError VALUE as the target. The message stays on Error.
func (e *Error) As(target any) bool {
	se, ok := target.(*parse.SyntaxError)
	if ok {
		*se = e.syntax()
	}
	return ok
}

// Is reports a syntax error's identities: parse.ErrUnterminated for an Incomplete file, otherwise
// parse.ErrSyntax, and through either errs.ErrInvalidArgument. Never both: a wrapped YAML error is
// classified for the file, not for the frontmatter on its own (see [Parse]).
func (e *Error) Is(target error) bool { return errors.Is(e.syntax(), target) }

func (e *Error) syntax() parse.SyntaxError {
	return parse.SyntaxError{Format: "view", Pos: e.Pos, Incomplete: e.Incomplete}
}

// Option configures [Parse].
type Option func(*config)

type config struct {
	name     string
	maxDepth int
}

// WithName names the file in every position and error, as in "track.view:12:3".
func WithName(name string) Option { return func(c *config) { c.name = name } }

// MaxDepth bounds how deeply the frontmatter's collections may nest; see the YAML parser's option of
// the same name, which it is handed to.
func MaxDepth(n int) Option { return func(c *config) { c.maxDepth = n } }

// Parse reads a .view file: a line holding exactly "---", the YAML frontmatter, the next line holding
// exactly "---", and the body template, which is everything after it and may itself begin with "---"
// (a Markdown document's own frontmatter). Lines end at "\n"; a "\r" before it is tolerated on the
// delimiter lines, so a file saved with CRLF line endings parses.
//
// Parse decides what the file IS, not what it means: the frontmatter is a YAML tree with no field
// checked and no tag resolved, and the body is a template tree with no function name checked. Both
// are golib/view's to evaluate. An ill-formed file is an *[Error] whose position is the file's own:
// a YAML error's line counts from the top of the file, not from the frontmatter. A template error is
// placed at the body's first line, and its message, the template parser's, names the line in the
// file where the parser stopped: "template: body:12: unexpected EOF" is line 12 of the file.
func Parse(src []byte, opts ...Option) (*File, error) {
	cfg := config{maxDepth: yaml.DefaultMaxDepth}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	f := &File{Source: src, Name: cfg.name, lines: lineStarts(src)}

	open, ok := delimiter(src, 0)
	if !ok {
		// A source that is so far only part of "---" is an opening not yet typed in full.
		cut := bytes.IndexByte(src, '\n') < 0 && strings.HasPrefix("---", string(src))
		return nil, f.errorAt(0, "the first line must be \"---\", opening the frontmatter", nil, cut)
	}
	f.Open = open
	end, found := Span{}, false
	for off := next(src, open.End); off < len(src) && !found; {
		end, found = delimiter(src, off)
		off = next(src, end.End)
	}
	if !found {
		return nil, f.errorAt(len(src), "the frontmatter has no closing \"---\" line", nil, true)
	}
	f.Close = end
	f.Front = Span{next(src, open.End), end.Start}
	f.Body = Span{next(src, end.End), len(src)}

	if err := f.parseMeta(cfg); err != nil {
		return nil, err
	}
	if err := f.parseBody(); err != nil {
		return nil, err
	}
	return f, nil
}

// Position resolves an offset of Source to a line and a column; the column counts characters, as an
// editor does. It only reads the File, so concurrent calls are safe.
func (f *File) Position(off int) parse.Position {
	off = max(0, min(off, len(f.Source)))
	i := sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > off }) - 1
	return parse.Position{Offset: off, Line: i + 1, Column: utf8.RuneCount(f.Source[f.lines[i]:off]) + 1, File: f.Name}
}

// MetaPosition resolves an offset into the frontmatter, as [File.Meta]'s spans give, to a position of
// the file.
func (f *File) MetaPosition(off int) parse.Position { return f.Position(f.Front.Start + off) }

// TemplatePosition resolves a template node's Pos to a position of the file. The template parser
// places an action at its first token, not at its "{{": {{.title}} is where ".title" begins.
func (f *File) TemplatePosition(pos tparse.Pos) parse.Position {
	return f.Position(f.Body.Start + int(pos) - f.pad)
}

func (f *File) parseMeta(cfg config) error {
	front := f.Source[f.Front.Start:f.Front.End]
	st, err := yaml.Parse(front, yaml.MaxDepth(cfg.maxDepth))
	if err != nil {
		// The frontmatter is closed by the time it is parsed, so text appended to the FILE cannot
		// finish a construct the YAML left open: for the file, the error is where it stands. The
		// wrapped YAML error says the same, or errors.Is would find ErrUnterminated through it
		// beside this error's ErrSyntax.
		var ye *yaml.Error
		if errors.As(err, &ye) {
			inFile := *ye
			inFile.Incomplete = false
			return f.errorAt(f.Front.Start+ye.Pos.Offset, ye.Msg, &inFile, false)
		}
		return f.errorAt(f.Front.Start, err.Error(), err, false)
	}
	switch len(st.Docs) {
	case 0:
	case 1:
		f.Meta = st.Docs[0]
	default:
		return f.errorAt(f.Front.Start+st.Docs[1].Span.Start, "the frontmatter holds more than one YAML document", nil, false)
	}
	return nil
}

// parseBody parses the body behind as many newlines as there are lines above it, so that the line
// numbers the template parser counts, and writes into its errors, are the file's. The padding is
// whitespace before the body's first byte; it is removed from the tree again, so executing the tree
// prints the body exactly as written.
func (f *File) parseBody() error {
	f.pad = f.Position(f.Body.Start).Line - 1
	padding := strings.Repeat("\n", f.pad)

	t := tparse.New(BodyTemplate)
	t.Mode = tparse.SkipFuncCheck
	trees := map[string]*tparse.Tree{}
	body := string(f.Source[f.Body.Start:f.Body.End])
	if _, err := t.Parse(padding+body, "", "", trees); err != nil {
		return f.errorAt(f.Body.Start, err.Error(), err, bodyIncomplete(body))
	}
	if body := trees[BodyTemplate]; body != nil && body.Root != nil && len(body.Root.Nodes) > 0 {
		// A "{{-" opening the body trims the padding with the rest of the leading space, so the
		// first node holds it only when nothing trimmed it.
		if text, ok := body.Root.Nodes[0].(*tparse.TextNode); ok && bytes.HasPrefix(text.Text, []byte(padding)) {
			text.Text = text.Text[f.pad:]
			text.Pos += tparse.Pos(f.pad)
			if len(text.Text) == 0 {
				body.Root.Nodes = body.Root.Nodes[1:]
			}
		}
	}
	f.Templates = trees
	return nil
}

func (f *File) errorAt(off int, msg string, err error, incomplete bool) *Error {
	return &Error{Pos: f.Position(off), Msg: msg, Err: err, Incomplete: incomplete}
}

// bodyClosers end what a body may have left open at the end of the file: nothing, an action, an
// action inside a parenthesis, a string in either quoting, a character literal, a comment.
var bodyClosers = []string{"", "}}", ")}}", "\"}}", "\")}}", "`}}", "'}}", "*/}}"}

// maxOpenBlocks bounds how many {{end}}s bodyIncomplete appends: deeper than a body is written by
// hand, and small enough that the check stays cheap on the error path.
const maxOpenBlocks = 16

// bodyIncomplete reports whether a body that failed to parse is the beginning of one that would:
// whether closing what it left open (one of bodyClosers, then up to maxOpenBlocks {{end}}s for its
// open if, range, with, define and block) makes it parse. The template parser does not say whether
// it stopped at the end of the input, so the question is asked of the text directly, and answered
// yes only by a completion that parses. A body broken in a way no such completion repairs, such as
// an {{end}} with nothing to end or an {{if}} with no condition, stays wrong where it stands.
//
// NOTE: the completions are bounded, so recall is too: a body cut inside parentheses nested two deep,
// or with more than maxOpenBlocks blocks open, is reported wrong rather than unfinished. Both are
// beyond what a body is written with by hand, and the error is still reported, only one keystroke
// early.
func bodyIncomplete(body string) bool {
	blocks := min(strings.Count(body, "{{"), maxOpenBlocks)
	for _, closer := range bodyClosers {
		for k := 0; k <= blocks; k++ {
			if closer == "" && k == 0 {
				continue
			}
			t := tparse.New("completion")
			t.Mode = tparse.SkipFuncCheck
			if _, err := t.Parse(body+closer+strings.Repeat("{{end}}", k), "", "", map[string]*tparse.Tree{}); err == nil {
				return true
			}
		}
	}
	return false
}

// delimiter reports whether the line starting at off is a frontmatter delimiter, and its span.
func delimiter(src []byte, off int) (Span, bool) {
	end := bytes.IndexByte(src[off:], '\n')
	if end < 0 {
		end = len(src)
	} else {
		end += off
	}
	line := bytes.TrimSuffix(src[off:end], []byte("\r"))
	return Span{off, end}, string(line) == "---"
}

// next returns the offset of the line after the one ending at end, the position of its "\n" or the
// end of the source.
func next(src []byte, end int) int {
	if end < len(src) {
		return end + 1
	}
	return end
}

// lineStarts returns the offset where each line of src starts.
func lineStarts(src []byte) []int {
	out := []int{0}
	for i, b := range src {
		if b == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}
