//go:build !unix

package diag

// noFollow is 0 where O_NOFOLLOW is not offered.
const noFollow = 0

// checkDir checks nothing where file ownership and modes are not unix's.
func checkDir(string) error { return nil }
