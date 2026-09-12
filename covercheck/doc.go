// Package covercheck compares Go coverage profiles across source revisions.
//
// The package is CI-provider neutral. Callers supply base and head profiles
// plus changed source ranges, then optionally evaluate the resulting report
// against a policy. Package covercheck never runs tests, invokes Git, changes a
// checkout, or performs network I/O.
package covercheck
