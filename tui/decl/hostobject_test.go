package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// connection is a consumer's own host object: decl does not know its type.
type connection struct{ name string }

func (*connection) HostObject() {}

// A host object decl does not list passes through Value as an object, by marking itself a
// HostObject, and the consumer's type takes it back from Obj. A value that is neither a kind decl
// knows nor a HostObject is still refused, naming what is wanted.
func TestValuePassesAHostObjectThrough(t *testing.T) {
	c := &connection{name: "prod"}
	v, err := tuidecl.Value(c)
	if err != nil || v.Kind != qml.SpecValueObject || v.Obj != any(c) {
		t.Fatalf("Value(a HostObject) = %+v, %v; want it as an object, the same value", v, err)
	}
	if _, err := tuidecl.Value(struct{ n int }{1}); err == nil || !strings.Contains(err.Error(), "HostObject") {
		t.Errorf("Value(an unknown struct): %v, want refused, naming HostObject", err)
	}
}
