package lexical

import "strconv"

// Length-prefix strings preserve arbitrary bytes, including malformed source UTF-8.
func contextKey(c context) string {
	var b []byte
	integer := func(n int) { b = strconv.AppendInt(b, int64(n), 10); b = append(b, ';') }
	str := func(s string) { integer(len(s)); b = append(b, s...) }
	flag := func(v bool) {
		if v {
			integer(1)
		} else {
			integer(0)
		}
	}
	str(c.Mode)
	str(c.Close)
	str(c.Open)
	integer(int(c.Escape))
	integer(int(c.Style))
	flag(c.Multiline)
	flag(c.Continuation)
	flag(c.Nested)
	flag(c.Doubling)
	integer(c.Depth)
	str(c.Last)
	integer(c.Scalar)
	str(c.Unit)
	integer(len(c.Templates))
	for _, v := range c.Templates {
		integer(v)
	}
	integer(len(c.Stack))
	for _, v := range c.Stack {
		str(v.Kind)
		str(v.Prefix)
		str(v.Close)
	}
	integer(len(c.Here))
	for _, v := range c.Here {
		str(v.Word)
		flag(v.Tabs)
	}
	integer(len(c.ExpressionParens))
	for _, v := range c.ExpressionParens {
		flag(v)
	}
	integer(len(c.Markup))
	for _, v := range c.Markup {
		integer(v.Depth)
		str(v.Mode)
		str(v.Resume)
		flag(v.Closing)
		integer(v.ExpressionDepth)
		integer(int(v.Quote))
		flag(v.Name)
	}
	return string(b)
}
