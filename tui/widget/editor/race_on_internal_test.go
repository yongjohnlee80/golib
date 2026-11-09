//go:build race

package editor

// raceBuild is whether this test binary is built with -race, which allocates more for the same
// work: the allocation budgets read it.
const raceBuild = true
