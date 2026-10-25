package qml

import (
	"strings"
	"testing"
)

func TestNew_SetsItsOwnFields(t *testing.T) {
	t.Parallel()
	if got, want := New(MaxDepth(5), WithName("Quit.qml"), nil), (QML{maxDepth: 5, file: "Quit.qml"}); got != want {
		t.Errorf("New = %+v, want %+v", got, want)
	}
	if got := New(); got != (QML{}) {
		t.Errorf("New() = %+v, want the zero value", got)
	}
	// The name reaches the positions in an error.
	_, err := New(WithName("Quit.qml")).Parse([]byte("Item {"))
	if err == nil || !strings.Contains(err.Error(), "Quit.qml:") {
		t.Errorf("err = %v, want a position naming Quit.qml", err)
	}
}
