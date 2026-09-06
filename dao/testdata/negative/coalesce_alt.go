// This file must NOT compile. It is the negative half of the expression
// surface: [dao.Coalesce] takes two [dao.Expr] values, and anything else is a
// rejection Go reports at BUILD time — which is stronger than a runtime panic
// and is the whole reason the string overload was removed.
//
// The removed overload accepted a bare Go string. That read well, but it made
// dao responsible for turning a string into a SQL literal, which has no
// portable answer (MySQL's escaping depends on session state), so it resolved
// the hard cases by panicking at render time. Requiring an Expr moves every one
// of those rejections to the compiler, and leaves the literal's spelling with
// the author: dao.Int for a number, dao.SQL for anything else.
//
// It lives under testdata/ so the go tool skips it for every package pattern;
// expr_negative_test.go builds this path explicitly and asserts it FAILS.
package negative

import "github.com/yongjohnlee80/golib/dao"

type artistField string

const aName artistField = "name"

// A bare string is the case that used to compile and then panic at render time
// on any dialect that had not stated a quoting rule.
func bareString() { _ = dao.Coalesce(dao.T("artist", aName), "n/a") }

// A bare int used to be accepted too. dao.Int(0) is the spelling now.
func bareInt() { _ = dao.Coalesce(dao.T("artist", aName), 0) }

func float() { _ = dao.Coalesce(dao.T("artist", aName), 0.5) }

func boolean() { _ = dao.Coalesce(dao.T("artist", aName), true) }

// A field enum where a VALUE belongs — the mistake worth catching, and one a
// string overload could never have caught, because a ~string enum IS a string.
func columnEnumAsValue() { _ = dao.Coalesce(dao.T("artist", aName), aName) }

func structValue() { _ = dao.Coalesce(dao.T("artist", aName), struct{}{}) }
