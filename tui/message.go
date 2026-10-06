package tui

// Message is text named by a translation-catalog id, shown in the App's language.
//
// A widget given a Message looks it up each time it is laid out, through
// [Context.Translate], so it follows [App.SetLanguage] with nothing to republish. Text that
// is not a Message is shown exactly as given: a label an application passes as a string is
// never translated.
//
// Message is comparable: two Messages are equal when their ids are.
type Message struct {
	// ID names the message in the catalogs ("tui.button.yes", "editor.menu.file").
	ID string
}

// Msg is the Message named id.
func Msg(id string) Message { return Message{ID: id} }
