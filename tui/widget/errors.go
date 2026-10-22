package widget

import (
	"strconv"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// PACKAGE ERRORS: the helpers that build the package's errs.Fatal values and validate a button
// list, shared by every widget that refuses a misuse.

// tuiFatal builds the package's standard construction panic.
func tuiFatal(op, rule string, got int) error {
	return fatalOf(op, rule, "got "+strconv.Itoa(got))
}

// fatalOf builds the package's standard construction panic value.
func fatalOf(op, rule, detail string) error {
	return errs.Fatal{Op: op, Rule: rule, Detail: detail}
}

// checkButtonList is the CONSTRUCTION adapter over validateButtonList.
//
// A panic rather than a silent precedence rule: with two defaults, Enter would
// pick one by an ordering the author never stated, and the dialog would do the
// wrong thing in a way that reads as a toolkit bug rather than a mistake in the
// button list. Construction options have no error to return, and the list is
// written in source, so this is a programmer error.
//
// It shares its rules with the runtime setter's typed error rather than
// restating them, so the two cannot come to disagree about what a valid list is.
func checkButtonList(op string, buttons []*Button, owner tui.Component) {
	if f := validateButtonList(buttons, owner); f != nil {
		panic(errs.Fatal{Op: op, Rule: f.describe()})
	}
}
