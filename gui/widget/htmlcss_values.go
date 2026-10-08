package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
)

// CSS MATHS — calc(), min(), max() and clamp() over lengths and numbers, with + - * / and
// parentheses, as a page sizes itself to the view: width: min(100% - 2.5rem, 940px);
// padding: max(1.25rem, calc((100vw - 1040px) / 2)); font-size: clamp(2rem, 5vw, 3.3rem). An
// expression is kept as written and evaluated where its length is, against the font, the root's
// font, what % is of, and the view (vw, vh).

// maxExprNodes bounds an expression, so a hostile value cannot make one that is costly to evaluate.
const maxExprNodes = 64

// cssExpr is a node of an expression: a leaf length, or an operator or function over its args.
type cssExpr struct {
	op   byte // 0 a leaf, '+' '-' '*' '/', 'm' min, 'M' max, 'c' clamp
	leaf cssLen
	num  bool // a leaf that is a bare number (a factor), not a length
	args []*cssExpr
}

func (e *cssExpr) eval(font, root, of float32, view gui.Size) float32 {
	switch e.op {
	case 0:
		if e.num {
			return e.leaf.v
		}
		return e.leaf.px(font, root, of, view)
	case '+':
		return e.args[0].eval(font, root, of, view) + e.args[1].eval(font, root, of, view)
	case '-':
		return e.args[0].eval(font, root, of, view) - e.args[1].eval(font, root, of, view)
	case '*':
		return e.args[0].eval(font, root, of, view) * e.args[1].eval(font, root, of, view)
	case '/':
		if d := e.args[1].eval(font, root, of, view); d != 0 {
			return e.args[0].eval(font, root, of, view) / d
		}
		return 0
	case 'm', 'M':
		v := e.args[0].eval(font, root, of, view)
		for _, a := range e.args[1:] {
			w := a.eval(font, root, of, view)
			if e.op == 'm' {
				v = min(v, w)
			} else {
				v = max(v, w)
			}
		}
		return v
	case 'c':
		lo, v, hi := e.args[0].eval(font, root, of, view), e.args[1].eval(font, root, of, view), e.args[2].eval(font, root, of, view)
		return max(lo, min(v, hi))
	}
	return 0
}

// parseExpr reads a function value (calc(…), min(…), max(…), clamp(…)); false for anything else
// or for one over the bound.
func parseExpr(s string) (*cssExpr, bool) {
	p := &exprParser{toks: exprTokens(s)}
	e, ok := p.function()
	if !ok || p.i != len(p.toks) || p.nodes > maxExprNodes {
		return nil, false
	}
	return e, true
}

type exprParser struct {
	toks  []string
	i     int
	nodes int
}

func (p *exprParser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}

func (p *exprParser) node(e *cssExpr) *cssExpr {
	p.nodes++
	return e
}

// function reads name( args ).
func (p *exprParser) function() (*cssExpr, bool) {
	name := p.peek()
	var op byte
	switch name {
	case "calc(":
		op = 0
	case "min(":
		op = 'm'
	case "max(":
		op = 'M'
	case "clamp(":
		op = 'c'
	default:
		return nil, false
	}
	p.i++
	var args []*cssExpr
	for {
		e, ok := p.sum()
		if !ok {
			return nil, false
		}
		args = append(args, e)
		switch p.peek() {
		case ",":
			p.i++
			continue
		case ")":
			p.i++
		default:
			return nil, false
		}
		break
	}
	switch {
	case name == "calc(" && len(args) == 1:
		return args[0], true
	case op == 'c' && len(args) == 3, (op == 'm' || op == 'M') && len(args) >= 1:
		return p.node(&cssExpr{op: op, args: args}), true
	}
	return nil, false
}

func (p *exprParser) sum() (*cssExpr, bool) {
	l, ok := p.product()
	for ok && (p.peek() == "+" || p.peek() == "-") {
		op := p.peek()[0]
		p.i++
		var r *cssExpr
		if r, ok = p.product(); ok {
			l = p.node(&cssExpr{op: op, args: []*cssExpr{l, r}})
		}
	}
	return l, ok && p.nodes <= maxExprNodes
}

func (p *exprParser) product() (*cssExpr, bool) {
	l, ok := p.factor()
	for ok && (p.peek() == "*" || p.peek() == "/") {
		op := p.peek()[0]
		p.i++
		var r *cssExpr
		if r, ok = p.factor(); ok {
			l = p.node(&cssExpr{op: op, args: []*cssExpr{l, r}})
		}
	}
	return l, ok
}

func (p *exprParser) factor() (*cssExpr, bool) {
	t := p.peek()
	switch {
	case t == "(":
		p.i++
		e, ok := p.sum()
		if !ok || p.peek() != ")" {
			return nil, false
		}
		p.i++
		return e, true
	case strings.HasSuffix(t, "("):
		return p.function()
	case t == "":
		return nil, false
	}
	p.i++
	if f, err := strconv.ParseFloat(t, 32); err == nil {
		return p.node(&cssExpr{num: true, leaf: cssLen{v: float32(f), unit: 'p'}}), true
	}
	l, ok := parseLen(t)
	if !ok || l.unit == 'a' || l.expr != nil {
		return nil, false
	}
	return p.node(&cssExpr{leaf: l}), true
}

// exprTokens splits an expression: "name(" for a function, "(" ")" "," and the operators, and
// lengths and numbers (a sign that starts a number stays on it: "-1px", "1e-3").
func exprTokens(s string) []string {
	var out []string
	s = strings.ToLower(s)
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(' || c == ')' || c == ',' || c == '*' || c == '/':
			out = append(out, string(c))
			i++
		case (c == '+' || c == '-') && !(i+1 < len(s) && (s[i+1] >= '0' && s[i+1] <= '9' || s[i+1] == '.') && startsOperand(out)):
			out = append(out, string(c))
			i++
		default:
			j := i + 1
			for j < len(s) && !strings.ContainsRune(" \t\n(),*/", rune(s[j])) && !(s[j] == '+' || s[j] == '-') || j < len(s) && (s[j] == '+' || s[j] == '-') && (s[j-1] == 'e' && j >= 2 && s[j-2] >= '0' && s[j-2] <= '9') {
				j++
			}
			if j < len(s) && s[j] == '(' {
				out = append(out, s[i:j+1]) // a function's name
				i = j + 1
				continue
			}
			out = append(out, s[i:j])
			i = j
		}
	}
	return out
}

// startsOperand reports whether a sign here starts a number: at the start, or after an operator,
// "(" or ",".
func startsOperand(toks []string) bool {
	if len(toks) == 0 {
		return true
	}
	switch t := toks[len(toks)-1]; {
	case t == "+" || t == "-" || t == "*" || t == "/" || t == "," || strings.HasSuffix(t, "("):
		return true
	}
	return false
}
