package widget

import "github.com/yongjohnlee80/golib/errs"

// fatalOf builds the package's standard construction panic value.
func fatalOf(op, rule, detail string) error {
	return errs.Fatal{Op: op, Rule: rule, Detail: detail}
}
