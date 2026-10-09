package lexical

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"strings"
)

func mappingKeys(line string, spans []highlight.Span, tokens []Token) []highlight.Span {
	styles := make([]highlight.Style, len(line))
	for _, span := range spans {
		for i := span.Start; i < span.End; i++ {
			styles[i] = span.Style
		}
	}
	start := len(indent.Leading(line))
	if strings.HasPrefix(line[start:], "- ") {
		start += 2
	}
	for _, token := range tokens {
		if token.Text == ":" && (token.End == len(line) || strings.ContainsRune(" \t{}[]", rune(line[token.End]))) {
			styles[token.Start] = highlight.Normal
			for i := start; i < token.Start; i++ {
				styles[i] = highlight.Attribute
			}
			break
		}
	}
	if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "..." {
		for i := start; i < len(line); i++ {
			styles[i] = highlight.RegionMarker
		}
	}
	var out []highlight.Span
	for i := 0; i < len(styles); {
		j := i + 1
		for j < len(styles) && styles[j] == styles[i] {
			j++
		}
		if styles[i] != highlight.Normal {
			out = append(out, highlight.Span{Start: i, End: j, Style: styles[i]})
		}
		i = j
	}
	return out
}
