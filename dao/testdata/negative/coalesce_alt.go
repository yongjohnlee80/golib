// This file must NOT compile. It is the negative half of ADR-0016 §2.3: the Alt
// constraint accepts an Expr, a string or an integer, and nothing else — a
// rejection Go reports at build time, which is stronger than a runtime panic.
//
// It lives under testdata/ so the go tool skips it for every package pattern;
// expr_negative_test.go builds this path explicitly and asserts it FAILS.
package negative

import "github.com/yongjohnlee80/golib/dao"

type artistField string

const aName artistField = "name"

func float() { _ = dao.Coalesce(dao.T("artist", aName), 0.5) }

func boolean() { _ = dao.Coalesce(dao.T("artist", aName), true) }

// A field enum where a VALUE belongs: ~string is deliberately not an Alt term,
// so this is the mistake the constraint is meant to catch.
func columnEnumAsValue() { _ = dao.Coalesce(dao.T("artist", aName), aName) }

func structValue() { _ = dao.Coalesce(dao.T("artist", aName), struct{}{}) }
