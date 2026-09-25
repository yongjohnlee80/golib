// Package highlight is the contract between whatever colours source text and
// whatever paints it: a Highlighter turns one line into styled spans.
//
// It mirrors Qt's QSyntaxHighlighter. Qt calls highlightBlock(text) once per
// line, and a highlighter reads previousBlockState() and sets
// setCurrentBlockState() so a construct that spans lines — a block comment, a
// multi-line string — continues on the next. Here that is one call:
//
//	spans, next := h.HighlightBlock(line, previous)
//
// The style a span carries is KSyntaxHighlighting's TextStyle — the set KDE's
// editors and Qt Creator colour every language with — so a theme colours all
// of them from one table, and a new language adds no style.
//
// It depends on nothing: a parser implements a Highlighter, a widget paints
// one, and neither imports the other. A program that already has a syntax tree
// — tree-sitter's, say — implements Highlighter from it; [StyleForCapture]
// maps tree-sitter's capture names, and a span keeps the name it came from.
package highlight

import (
	"strconv"
	"strings"
)

// Style is what a span of text is, for colouring: KSyntaxHighlighting's
// TextStyle, every value, in its order.
type Style uint8

const (
	Normal         Style = iota // text with no special highlighting
	Keyword                     // a language keyword
	Function                    // a function definition or call
	Variable                    // a variable, where a language marks them
	ControlFlow                 // if, else, return, continue
	Operator                    // + - * / ::
	BuiltIn                     // a built-in class or function
	Extension                   // a well-known extension: Qt, boost
	Preprocessor                // a preprocessor statement
	Attribute                   // an attribute of a function or object
	Char                        // a single character: 'a'
	SpecialChar                 // an escape inside a string: \n
	String                      // a string
	VerbatimString              // a verbatim string: a HERE doc
	SpecialString               // a regular expression, LaTeX maths
	Import                      // an include, import or module
	DataType                    // a data type: int, char, a type name
	DecVal                      // a decimal number
	BaseN                       // a number in a base other than 10
	Float                       // a floating-point number
	Constant                    // a language constant: true, null
	Comment                     // a comment
	Documentation               // a documentation comment
	Annotation                  // an annotation in a comment: \a
	CommentVar                  // a variable named in a comment
	RegionMarker                // a region marker: BEGIN/END
	Information                 // \note
	Warning                     // \warning
	Alert                       // TODO, FIXME
	Error                       // wrong syntax
	Others                      // anything the rest do not cover
	styleCount
)

// Styles is the number of styles: a table indexed by Style has this length.
const Styles = int(styleCount)

var styleNames = [...]string{
	"normal", "keyword", "function", "variable", "controlFlow", "operator",
	"builtIn", "extension", "preprocessor", "attribute", "char", "specialChar",
	"string", "verbatimString", "specialString", "import", "dataType", "decVal",
	"baseN", "float", "constant", "comment", "documentation", "annotation",
	"commentVar", "regionMarker", "information", "warning", "alert", "error",
	"others",
}

// String is the style's name as KDE's theme files spell it, lower-camel:
// "controlFlow", "dataType".
func (s Style) String() string {
	if int(s) < len(styleNames) {
		return styleNames[s]
	}
	return "Style(" + strconv.Itoa(int(s)) + ")"
}

// StyleNamed is the Style a name spells, as String writes it.
func StyleNamed(name string) (Style, bool) {
	for i, n := range styleNames {
		if n == name {
			return Style(i), true
		}
	}
	return 0, false
}

// State is carried from the end of one line to the start of the next — Qt's
// block state. Zero is "nothing open"; a highlighter gives its other values
// their meaning (inside a block comment, inside a template string).
type State int

// Span is one styled run of a line, in BYTE offsets of the line, half-open:
// [Start, End). Bytes, because a highlighter reads bytes and a syntax tree
// reports them — tree-sitter's nodes are byte ranges — while grapheme
// clusters are the painter's business; the Editor maps one to the other.
type Span struct {
	Start, End int
	Style      Style
	// Name is a finer name for the span, when its source has one — a
	// tree-sitter capture, "keyword.control.import". Painting uses Style;
	// Name is kept so nothing a richer source knew is thrown away.
	Name string
}

// Highlighter colours one line at a time.
//
// It must never refuse. Text being typed is invalid most of the time; an
// unterminated string or comment is a State carried to the next line, and
// anything unrecognised is Normal. Spans may leave columns uncovered (Normal)
// and must not overlap or reach past the line.
type Highlighter interface {
	HighlightBlock(line string, previous State) (spans []Span, next State)
}

// HighlighterFunc is a Highlighter written as a function.
type HighlighterFunc func(line string, previous State) ([]Span, State)

// HighlightBlock implements Highlighter.
func (f HighlighterFunc) HighlightBlock(line string, previous State) ([]Span, State) {
	return f(line, previous)
}

// captureStyles map tree-sitter's standard highlight captures to Styles. A
// capture is matched by its longest known prefix: "keyword.control.import"
// finds "keyword.control" before "keyword".
var captureStyles = map[string]Style{
	"attribute":             Attribute,
	"boolean":               Constant,
	"character":             Char,
	"character.special":     SpecialChar,
	"comment":               Comment,
	"comment.doc":           Documentation,
	"comment.documentation": Documentation,
	"conditional":           ControlFlow,
	"constant":              Constant,
	"constant.builtin":      Constant,
	"constructor":           DataType,
	"embedded":              Normal,
	"error":                 Error,
	"exception":             ControlFlow,
	"field":                 Attribute,
	"float":                 Float,
	"function":              Function,
	"function.builtin":      BuiltIn,
	"function.call":         Function,
	"function.macro":        Preprocessor,
	"function.method":       Function,
	"include":               Import,
	"keyword":               Keyword,
	"keyword.conditional":   ControlFlow,
	"keyword.control":       ControlFlow,
	"keyword.exception":     ControlFlow,
	"keyword.function":      Keyword,
	"keyword.import":        Import,
	"keyword.operator":      Operator,
	"keyword.repeat":        ControlFlow,
	"keyword.return":        ControlFlow,
	"label":                 Attribute,
	"method":                Function,
	"module":                Import,
	"namespace":             Import,
	"number":                DecVal,
	"number.float":          Float,
	"operator":              Operator,
	"parameter":             Variable,
	"preproc":               Preprocessor,
	"property":              Attribute,
	"punctuation":           Normal,
	"repeat":                ControlFlow,
	"string":                String,
	"string.escape":         SpecialChar,
	"string.regex":          SpecialString,
	"string.regexp":         SpecialString,
	"string.special":        SpecialString,
	"tag":                   DataType,
	"tag.attribute":         Attribute,
	"text.todo":             Alert,
	"todo":                  Alert,
	"type":                  DataType,
	"type.builtin":          DataType,
	"variable":              Variable,
	"variable.builtin":      BuiltIn,
	"variable.parameter":    Variable,
}

// StyleForCapture is the Style for a tree-sitter highlight capture — with or
// without its leading "@" — by its longest known prefix, and Normal for one it
// does not know.
func StyleForCapture(capture string) Style {
	name := strings.TrimPrefix(capture, "@")
	for name != "" {
		if s, ok := captureStyles[name]; ok {
			return s
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return Normal
}
