package widget

import (
	"reflect"
	"testing"
)

// Moved from gui/widget with the classifier it checks (MarkdownMetadata).

func TestMetadataFastEditsMatchFreshParserMasks(t *testing.T) {
	for _, c := range []struct{ old, new []string }{
		{[]string{"head", "ordinary [[link]] and `code` words", "<!-- note -->"}, []string{"head", "ordinary [[link]] and `code` new words", "<!-- note -->"}},
		{[]string{"<!-- note", "body", "-->", "tail"}, []string{"<!-- note", "longer body", "-->", "tail"}},
		{[]string{"before <!-- note", "end", ""}, []string{"before <!-- note", "end -->", ""}},
		{[]string{"text", "```", "<!-- literal -->", "```"}, []string{"text", "ordinary", "<!-- literal -->", "```"}},
		{[]string{"before <!-- note", "===", "end -->"}, []string{"before <!-- note", "words", "end -->"}},
		{[]string{"head", "`a` <!-- note -->", "tail"}, []string{"head", "`` <!-- note -->", "tail"}},
		{[]string{"x <!-- note", "abc", "-->"}, []string{"x <!-- note", "|---|", "-->"}},
		{[]string{"x <!-- note", "abc", "-->"}, []string{"x <!-- note", "|:---:|", "-->"}},
		{[]string{"x <!-- note", "| |", "-->"}, []string{"x <!-- note", "|---|", "-->"}},
		{[]string{"x <!-- note", "|  |", "-->"}, []string{"x <!-- note", "|---|", "-->"}},
		{[]string{"x <!-- note", "|---|", "-->"}, []string{"x <!-- note", "| |", "-->"}},
		{[]string{"x <!-- note", "|---|", "-->"}, []string{"x <!-- note", "|  |", "-->"}},
	} {
		var incremental, fresh MarkdownMetadata
		incremental.Prepare(c.old)
		incremental.Prepare(c.new)
		fresh.Prepare(c.new)
		if !reflect.DeepEqual(incremental.clusters, fresh.clusters) || !reflect.DeepEqual(incremental.hidden, fresh.hidden) || !reflect.DeepEqual(incremental.continued, fresh.continued) {
			t.Errorf("incremental masks differ from fresh parse: %v -> %v", c.old, c.new)
		}
	}
}
