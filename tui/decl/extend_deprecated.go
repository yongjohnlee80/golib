package decl

import "github.com/yongjohnlee80/golib/parse/qml"

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// ReadProps reads a builder's constructor properties by name and returns the ones it read.
//
// Deprecated: use [Build.ReadProps], which also refuses a property the type does not declare, so a
// builder and its Type cannot drift apart. Not maintained; removed in the next minor version.
func ReadProps(props []qml.SpecProp, fields map[string]Field) ([]string, error) {
	return readProps(props, fields)
}
