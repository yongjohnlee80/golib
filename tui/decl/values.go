package decl

import (
	"strconv"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// The value makers: a Go value as a property, a signal argument or a getter gives it to QML. Every
// value decl makes is made here, so a kind is spelled one way.

// strValue is a string.
func strValue(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueString, Raw: s} }

// numberValue is an integer.
func numberValue(n int) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(n)}
}

// floatValue is a real number as short as it reads: 50, or 33.33 (a Drawer's placed fractions, a
// Tile's size).
func floatValue(f float64) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.FormatFloat(f, 'f', -1, 64)}
}

// boolValue is true or false.
func boolValue(b bool) qml.SpecValue {
	return qml.SpecValue{Kind: qml.SpecValueBool, Raw: strconv.FormatBool(b)}
}
