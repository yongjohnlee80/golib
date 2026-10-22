package decl_test

import (
	"testing"

	guidecl "github.com/yongjohnlee80/golib/gui/decl"
)

// A nil option is no option: Native skips it rather than call it.
func TestNativeSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Native with a nil option panicked: %v", r)
		}
	}()
	guidecl.Native(nil)
}
