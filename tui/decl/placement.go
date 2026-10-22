package decl

import "fmt"

// parentBound is a node that belongs only in some parents: allowedIn refuses a parent type it may
// not be declared in, "" being the root, self being the node's own type as written. Create asks
// it of every child, and VetRoot of the root, so the rule is stated once, by the node it is about.
type parentBound interface {
	allowedIn(self, parent string) error
}

// notOne says a parent is not the one a node belongs in: "Flex is not one", or for the root,
// which has no parent, "the root is in none".
func notOne(parent string) string {
	if parent == "" {
		return "the root is in none"
	}
	return parent + " is not one"
}

// allowedIn: a SyntaxHighlighter highlights the Editor it is declared in.
func (*syntaxNode) allowedIn(_, parent string) error {
	if parent == editorName {
		return nil
	}
	return fmt.Errorf("a SyntaxHighlighter highlights the Editor it is declared in, and %s", notOne(parent))
}

// allowedIn: a renderer draws the Editor it is declared in.
func (*rendererNode) allowedIn(self, parent string) error { return editorPart(self, parent) }

// allowedIn: a document view draws the Editor it is declared in.
func (*documentNode) allowedIn(self, parent string) error { return editorPart(self, parent) }

func editorPart(what, parent string) error {
	if parent == editorName {
		return nil
	}
	return fmt.Errorf("a %s draws the Editor it is declared in, and %s", what, notOne(parent))
}

// allowedIn: a Shortcut is live while focus is in the holder that declares it.
func (*shortcutNode) allowedIn(_, parent string) error {
	if shortcutHolders[parent] {
		return nil
	}
	where := parent + " is none of them"
	if parent == "" {
		where = "the root is in none"
	}
	return fmt.Errorf("a Shortcut is live while focus is in the element that declares it, a %s, "+
		"and %s: put it in a FocusScope", shortcutHolderNames(), where)
}

var (
	_ parentBound = (*syntaxNode)(nil)
	_ parentBound = (*rendererNode)(nil)
	_ parentBound = (*documentNode)(nil)
	_ parentBound = (*shortcutNode)(nil)
)
