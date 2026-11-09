package widget_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// Each of the editor's (its macros' too), the buffer view's and the menu model's conditions is one of errs' kinds, so
// a caller asks errors.Is of the kind as of the package's sentinel.
func TestTheWidgetConditionsAreErrsKinds(t *testing.T) {
	for _, c := range []struct {
		name      string
		err, kind error
	}{
		{"ErrUnknownCommand", editor.ErrUnknownCommand, errs.ErrInvalidArgument},
		{"ErrCommandExists", editor.ErrCommandExists, errs.ErrInvalidArgument},
		{"ErrNoWriter", editor.ErrNoWriter, errs.ErrUnsupported},
		{"ErrNoQuit", editor.ErrNoQuit, errs.ErrUnsupported},
		{"ErrReadOnly", editor.ErrReadOnly, errs.ErrPrecondition},
		{"ErrClosed", core.ErrClosed, errs.ErrClosed},
		{"ErrInvalidMenuModel", core.ErrInvalidMenuModel, errs.ErrInvalidArgument},
		{"ErrEmptyItemID", core.ErrEmptyItemID, errs.ErrInvalidArgument},
		{"ErrDuplicateItemID", core.ErrDuplicateItemID, errs.ErrInvalidArgument},
		{"ErrUnknownItemKind", core.ErrUnknownItemKind, errs.ErrInvalidArgument},
		{"ErrMacroTooLong", editor.ErrMacroTooLong, errs.ErrUnsupported},
	} {
		if !errors.Is(c.err, c.kind) {
			t.Errorf("%s is not %v", c.name, c.kind)
		}
	}
}

// RunCommand's refusal of a verb it does not have is ErrUnknownCommand and its kind, the verb kept.
func TestAnUnknownVerbIsErrUnknownCommand(t *testing.T) {
	c := editor.NewEditorCore()
	err := c.RunCommand("frob")
	if !errors.Is(err, editor.ErrUnknownCommand) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RunCommand(frob) = %v, want ErrUnknownCommand, an errs.ErrInvalidArgument", err)
	}
}
