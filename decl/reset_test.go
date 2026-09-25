package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// reset_test.go covers the optional Resetter capability — Qt's RESET: a
// property a reload removes is reset in place, keeping the node, when the
// adapter says it can be.

// resetter is a reactor that can reset Text's hint.
type resetter struct {
	*reactor
	resetErr error
}

func (r *resetter) Resettable(typeName, prop string) bool {
	return typeName == "Text" && prop == "hint"
}

func (r *resetter) Reset(n decl.NodeID, prop string) error {
	r.trace = append(r.trace, fmt.Sprintf("reset %d %s", n, prop))
	return r.resetErr
}

var _ decl.Resetter = (*resetter)(nil)

func TestARemovedResettablePropertyIsResetNotRebuilt(t *testing.T) {
	rec := &resetter{reactor: newReactor()}
	tr := treeWith(t, rec, rec.recorder, `Flex { Text { id: a; text: "t"; hint: y } }`, map[string]string{"y": "2"}, nil)
	a, _ := tr.NodeByID("a")
	rec.trace = nil

	res, err := tr.Reconcile(mustSpec(t, `Flex { Text { id: a; text: "t" } }`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rebuilt) != 0 || res.Reset != 1 {
		t.Fatalf("res = %+v, want one reset and no rebuild", res)
	}
	if after, _ := tr.NodeByID("a"); after != a {
		t.Errorf("the node was replaced: %d -> %d", a, after)
	}
	if got := strings.Join(rec.trace, "\n"); got != fmt.Sprintf("reset %d hint", a) {
		t.Errorf("trace:\n%s", got)
	}

	// The removed binding is gone with the declaration: its source moving
	// reaches nothing.
	rec.trace = nil
	if _, err := tr.SetSource("y", sv("3")); err != nil {
		t.Fatal(err)
	}
	if len(rec.trace) != 0 {
		t.Errorf("a removed binding still applied:\n%s", strings.Join(rec.trace, "\n"))
	}
}

// TestAResetRunsBeforeTheNodesOtherChanges: removing one property and
// changing another is one patch, the reset first.
func TestAResetRunsBeforeTheNodesOtherChanges(t *testing.T) {
	rec := &resetter{reactor: newReactor()}
	tr := treeWith(t, rec, rec.recorder, `Text { id: a; text: "t"; hint: "h" }`, nil, nil)
	a, _ := tr.NodeByID("a")
	rec.trace = nil
	if _, err := tr.Reconcile(mustSpec(t, `Text { id: a; text: "u" }`)); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("reset %d hint\napply %d text=string(u) from-schema", a, a)
	if got := strings.Join(rec.trace, "\n"); got != want {
		t.Errorf("trace:\n%s\nwant:\n%s", got, want)
	}
}

func TestAFailedResetIsReported(t *testing.T) {
	rec := &resetter{reactor: newReactor(), resetErr: errors.New("no")}
	tr := treeWith(t, rec, rec.recorder, `Text { text: "t"; hint: "h" }`, nil, nil)
	_, err := tr.Reconcile(mustSpec(t, `Text { text: "t" }`))
	var se decl.SchemaError
	if !errors.As(err, &se) || se.Op != "reset" || se.Detail != "hint" || !errors.Is(err, decl.ErrAdapter) {
		t.Fatalf("err = %v, want a reset SchemaError for hint wrapping ErrAdapter", err)
	}
}

// TestAPropertyThatCannotBeResetStillRebuilds: the capability is per
// property, and without it removal is the rebuild it always was.
func TestAPropertyThatCannotBeResetStillRebuilds(t *testing.T) {
	for name, r := range map[string]*reactor{"not resettable": newReactor(), "no capability": newReactor()} {
		var a decl.Adapter = r
		if name == "not resettable" {
			a = &resetter{reactor: r}
		}
		tr := treeWith(t, a, r.recorder, `Flex { Text { text: "t"; hint: "h" } }`, nil, nil)
		// Removing text, which the resetter cannot reset — or anything, for
		// the adapter without the capability.
		src := `Flex { Text { hint: "h" } }`
		if name == "no capability" {
			src = `Flex { Text { text: "t" } }`
		}
		res, err := tr.Reconcile(mustSpec(t, src))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Rebuilt) != 1 || res.Reset != 0 || !strings.Contains(res.Rebuilt[0].Reason, "removed") {
			t.Errorf("%s: res = %+v, want one rebuild for the removal", name, res)
		}
	}
}
