package html_test

// SpecVersion is the CommonMark version the rule checklist was written against.
const SpecVersion = "0.31.2"

// rule is one statement of the specification that the conformance cases prove. NoNegative, when
// set, says why the rule has no meaningful near miss; the coverage test reports every such
// exemption rather than hiding it.
type rule struct {
	ID         string
	Statement  string
	NoNegative string
}

// rules is the checklist. Ids are "<section>/<name>"; an interaction between two sections is
// "<a>x<b>/<name>". Every rule needs at least one positive and one negative case (or a NoNegative
// reason); coverage_test.go enforces it. Statements paraphrase https://spec.commonmark.org/0.31.2/.
var rules = []rule{
	{ID: "2.1/line-endings", Statement: "a line ends at LF, CR or CRLF", NoNegative: "every byte sequence has some line structure; there is no near miss"},
	{ID: "2.2/tab-stops", Statement: "for block structure a tab advances to the next multiple of 4 columns"},
	{ID: "2.2/tab-in-content-kept", Statement: "a tab inside content is kept as a tab", NoNegative: "content tabs have no near miss"},
	{ID: "2.2/partial-tab-becomes-spaces", Statement: "a tab partly consumed by block structure leaves its remainder as spaces", NoNegative: "the whole-tab case is 2.2/tab-stops"},
	{ID: "2.3/nul-replaced", Statement: "U+0000 is replaced by U+FFFD", NoNegative: "a NUL is always replaced"},
	{ID: "2.4/backslash-escape", Statement: "a backslash before ASCII punctuation makes it literal, but not before other characters or inside code"},
	{ID: "2.5/entity-reference", Statement: "HTML5 named, decimal (1–7 digits) and hex (1–6 digits) references decode; 0 is U+FFFD; anything else is text"},

	{ID: "4.1/thematic-break", Statement: "three or more matching -, _ or *, optionally separated by spaces or tabs"},
	{ID: "4.2/atx-heading", Statement: "1–6 '#' then a space, tab or line end open an ATX heading, which may be empty"},
	{ID: "4.2/closing-sequence", Statement: "an optional closing run of '#' preceded by a space or tab is dropped"},
	{ID: "4.3/setext-heading", Statement: "a paragraph followed by a line of '=' (h1) or '-' (h2), with no internal spaces"},
	{ID: "4.3x4.1/setext-before-thematic-break", Statement: "under a paragraph, '---' is a setext underline, not a thematic break"},
	{ID: "4.4/indented-code", Statement: "lines indented four or more columns form an indented code block, which cannot interrupt a paragraph"},
	{ID: "4.4/trailing-blank-lines-dropped", Statement: "blank lines at the end of an indented code block are not part of it", NoNegative: "interior blank lines are kept; there is no near miss at the end"},
	{ID: "4.5/fenced-code", Statement: "three or more backticks or tildes open a fenced code block, running to its end or its container's"},
	{ID: "4.5/info-string", Statement: "the text after the opening fence is the info string; a backtick fence's cannot contain a backtick"},
	{ID: "4.5/closing-fence", Statement: "the closing fence must use the same character and be at least as long"},
	{ID: "4.6/html-block-type1", Statement: "<pre, <script, <style or <textarea open a block that ends at an end tag of any of them, across blank lines"},
	{ID: "4.6/html-block-type2", Statement: "'<!--' opens a block ending at '-->'"},
	{ID: "4.6/html-block-type6", Statement: "a block tag name opens a block (it may interrupt a paragraph) ending at a blank line"},
	{ID: "4.6/html-block-type7", Statement: "a complete open or closing tag alone on its line opens a block, which cannot interrupt a paragraph"},
	{ID: "4.7/link-reference-definition", Statement: "[label]: destination \"title\" alone on its line(s) defines a link reference"},
	{ID: "4.7/label-matching", Statement: "labels match after Unicode case folding and whitespace collapsing"},
	{ID: "4.7/first-definition-wins", Statement: "when a label is defined twice, the first definition is used", NoNegative: "a second definition has no observable effect to miss"},
	{ID: "4.8/paragraph-lines", Statement: "consecutive non-blank lines form a paragraph, with leading whitespace stripped", NoNegative: "every non-blank text line joins a paragraph"},
	{ID: "4.9/blank-lines-separate", Statement: "blank lines between blocks are ignored except for list tightness", NoNegative: "blank lines have no near miss"},

	{ID: "5.1/block-quote", Statement: "lines starting with '>' form a block quote"},
	{ID: "5.1/laziness", Statement: "a paragraph continuation line may omit the '>', and only paragraph text may"},
	{ID: "5.1x5.2/lazy-line-in-list-in-quote", Statement: "laziness reaches through nested containers to the innermost open paragraph"},
	{ID: "5.1/blank-ends-quote", Statement: "a blank line without '>' ends a block quote"},
	{ID: "5.2/list-marker", Statement: "'-', '+' or '*', or 1–9 digits then '.' or ')', followed by a space, tab or line end"},
	{ID: "5.2/item-content-indent", Statement: "an item's content column is set by its first line; five or more spaces make indented code"},
	{ID: "5.2/interrupt-paragraph", Statement: "an item may interrupt a paragraph only if non-empty and, when ordered, starting at 1"},
	{ID: "5.3/list-continuity", Statement: "items with the same bullet or delimiter continue a list; a change starts a new one"},
	{ID: "5.3/loose-tight", Statement: "a blank line between items, or between blocks in an item, makes the list loose"},

	{ID: "6.1/code-span", Statement: "a backtick run and the next run of equal length enclose a code span; line endings become spaces"},
	{ID: "6.1/space-stripping", Statement: "one space is stripped from each side when both sides have one and the content is not all spaces"},
	{ID: "6.2/emphasis", Statement: "left-flanking runs open and right-flanking runs close emphasis; _ not inside words"},
	{ID: "6.2/rule-of-three", Statement: "a both-flanking delimiter cannot pair when the run lengths sum to a multiple of 3 unless both are"},
	{ID: "6.3/inline-link", Statement: "[text] directly followed by (destination \"title\")"},
	{ID: "6.3/links-do-not-nest", Statement: "links may not contain links (the inner one wins); images may"},
	{ID: "6.3/reference-link", Statement: "full, collapsed and shortcut references use a matching definition; an undefined full label is text"},
	{ID: "6.3/code-span-precedence", Statement: "a code span binds tighter than link syntax it overlaps, but may sit inside link text"},
	{ID: "6.4/image", Statement: "![alt](destination) is an image whose alt text is the plain text of its content"},
	{ID: "6.5/autolink", Statement: "<scheme:...> (scheme 2–32 characters) or <email> without spaces"},
	{ID: "6.6/raw-html", Statement: "a complete tag, comment, processing instruction, declaration or CDATA; tag names start with a letter"},
	{ID: "6.7/hard-break", Statement: "two or more spaces or a backslash before a line ending, not at the end of a block"},
	{ID: "6.9/text", Statement: "anything not otherwise interpreted is literal text", NoNegative: "text is the fallback; it has no near miss"},
}
