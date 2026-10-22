package widget_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Each of the editor's (its macros' too), the buffer view's and the menu model's conditions is one of errs' kinds, so
// a caller asks errors.Is of the kind as of the package's sentinel.
func TestTheWidgetConditionsAreErrsKinds(t *testing.T) {
	for _, c := range []struct {
		name      string
		err, kind error
	}{
		{"ErrUnknownCommand", widget.ErrUnknownCommand, errs.ErrInvalidArgument},
		{"ErrCommandExists", widget.ErrCommandExists, errs.ErrInvalidArgument},
		{"ErrNoWriter", widget.ErrNoWriter, errs.ErrUnsupported},
		{"ErrNoQuit", widget.ErrNoQuit, errs.ErrUnsupported},
		{"ErrReadOnly", widget.ErrReadOnly, errs.ErrPrecondition},
		{"ErrClosed", widget.ErrClosed, errs.ErrClosed},
		{"ErrInvalidMenuModel", widget.ErrInvalidMenuModel, errs.ErrInvalidArgument},
		{"ErrEmptyItemID", widget.ErrEmptyItemID, errs.ErrInvalidArgument},
		{"ErrDuplicateItemID", widget.ErrDuplicateItemID, errs.ErrInvalidArgument},
		{"ErrUnknownItemKind", widget.ErrUnknownItemKind, errs.ErrInvalidArgument},
		{"ErrMacroTooLong", widget.ErrMacroTooLong, errs.ErrUnsupported},
	} {
		if !errors.Is(c.err, c.kind) {
			t.Errorf("%s is not %v", c.name, c.kind)
		}
	}
}

// RunCommand's refusal of a verb it does not have is ErrUnknownCommand and its kind, the verb kept.
func TestAnUnknownVerbIsErrUnknownCommand(t *testing.T) {
	c := widget.NewEditorCore()
	err := c.RunCommand("frob")
	if !errors.Is(err, widget.ErrUnknownCommand) || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RunCommand(frob) = %v, want ErrUnknownCommand, an errs.ErrInvalidArgument", err)
	}
}
