package yaml_test

// skip lists the yaml-test-suite tests the parser cannot pass yet, by id, with why. It is a
// working list while the parser is written: TestNoSkips fails whenever it is not empty, so the
// package reaches main only when every test passes.
var skip = map[string]string{}
