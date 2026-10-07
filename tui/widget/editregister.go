package widget

// EDIT REGISTER — the unnamed register of a text widget: what was last yanked or deleted, and
// whether it was whole lines; and whether yanking is allowed at all. Exporting a yank to the
// system clipboard is the widget's (it holds the Context); the register only keeps the text.

type editRegister struct {
	text     string
	linewise bool
	yank     bool // yanking is allowed: copies reach the register and the system clipboard
}

// set fills the register, as a yank or a delete does.
func (r *editRegister) set(text string, linewise bool) { r.text, r.linewise = text, linewise }

// content is what the register holds.
func (r *editRegister) content() (text string, linewise bool) { return r.text, r.linewise }

// holds reports whether a paste would insert anything: text, or an empty line-wise yank.
func (r *editRegister) holds() bool { return r.text != "" || r.linewise }

// yankAllowed reports whether yanking is on.
func (r *editRegister) yankAllowed() bool { return r.yank }
