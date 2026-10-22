package widget

import (
	"testing"

	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// A nil option is no option, as tui's constructors take it: NewEditor, NewPanel and
// NewMarkdownRenderer skip it rather than call it.
func TestTheConstructorsSkipANilOption(t *testing.T) {
	for name, build := range map[string]func(){
		"NewEditor":           func() { NewEditor(nil, WithLineNumbers(true)) },
		"NewPanel":            func() { NewPanel(tuiwidget.NewText("x"), nil, WithTitle("t")) },
		"NewMarkdownRenderer": func() { NewMarkdownRenderer(nil, WithHeadingScale(2)) },
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s with a nil option panicked: %v", name, r)
				}
			}()
			build()
		}()
	}
}
