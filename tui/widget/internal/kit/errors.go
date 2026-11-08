package kit

import "github.com/yongjohnlee80/golib/errs"

// FatalOf builds the package's standard construction panic value.
func FatalOf(op, rule, detail string) error {
	return errs.Fatal{Op: op, Rule: rule, Detail: detail}
}
