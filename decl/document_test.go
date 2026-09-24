package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// document_test.go covers the checks that judge a WHOLE document before any of
// it is planned, and the lookup a host uses to reach a node by its id.

// TestTwoNodesCannotShareAnID.
//
// QML requires an id to be unique within a document, and the engine depends on
// it: NodeByID answers with ONE node, and a reload matches old nodes to new by
// id. With a duplicate both would pick whichever node a map iteration reached
// first — a different answer from run to run, with no error anywhere.
func TestTwoNodesCannotShareAnID(t *testing.T) {
	const doc = "Flex {\n  Text { id: a }\n  Text { id: a }\n}"

	err := decl.New(newReactor()).Mount(qmlDoc(t, doc))
	if !errors.Is(err, decl.ErrDuplicateID) {
		t.Fatalf("Mount: err = %v, want ErrDuplicateID", err)
	}
	for _, want := range []string{`"a"`, "2:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic = %q, want it to name %s", err, want)
		}
	}

	// The same rule on the OTHER way in. A check listed at one entry point and
	// not the other is how a reload comes to accept what a mount refuses.
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, "Flex { Text { id: a } }")
	if _, err := tr.Reconcile(qmlDoc(t, doc)); !errors.Is(err, decl.ErrDuplicateID) {
		t.Errorf("Reconcile: err = %v, want ErrDuplicateID", err)
	}
	if len(rec.trace) != 0 {
		t.Errorf("a refused reload touched the tree: %v", rec.trace)
	}
}

// TestNodeByIDFindsTheDeclaredNode, and nothing for an id no node declares.
func TestNodeByIDFindsTheDeclaredNode(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder, "Flex {\n  Text { id: first }\n  Text { id: second }\n}")

	id, ok := tr.NodeByID("second")
	if !ok {
		t.Fatal("NodeByID(second) found nothing")
	}
	if got, _ := tr.SchemaID(id); got != "second" {
		t.Errorf("NodeByID(second) returned the node declared %q", got)
	}
	if _, ok := tr.NodeByID("nosuch"); ok {
		t.Error("NodeByID found a node for an id nobody declared")
	}
	if _, ok := tr.NodeByID(""); ok {
		t.Error("NodeByID(\"\") matched a node with no id")
	}
}
