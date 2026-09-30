package themes

import (
	"strings"
	"testing"
	"testing/fstest"
)

// values refuses what is not a Theme of strings, naming why: the shipped
// themes never reach these, a copy a program edits can.
func TestValuesRefusesWhatIsNotAThemeOfStrings(t *testing.T) {
	fsys := fstest.MapFS{
		"broken.qml": {Data: []byte("Theme { app { window: ")},
		"flex.qml":   {Data: []byte(`Flex { app { window: "white" } }`)},
		"number.qml": {Data: []byte(`Theme { app { window: 4 } }`)},
		"good.qml":   {Data: []byte(`Theme { app { window: "white"; inactive { highlight: "gray" } } }`)},
	}
	for name, want := range map[string]string{
		"broken": "themes: ", "flex": "flex.qml is not a Theme", "number": "app.window is not a string",
	} {
		if _, err := values(fsys, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", name, err, want)
		}
	}
	v, err := values(fsys, "good")
	if err != nil || v["app.window"] != "white" || v["app.inactive.highlight"] != "gray" || len(v) != 2 {
		t.Errorf("good = %v, %v", v, err)
	}
}
