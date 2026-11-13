// Package mergeview is the terminal's merge view: two texts side by side and the result between
// them, as JetBrains' merge editor lays them out. The left and right panes are the two sources,
// read-only; the middle is the result, editable. Between each side and the result a connector
// marks each change's lines and holds its arrows (» take the left's, « take the right's, ×
// dismiss); a bar below holds Accept Left, Accept Right and Merge, the counts, Apply and Cancel.
//
// The view never loads or writes a text: a merge.MergeSource does, so a buffer against its file,
// a remote object or git's stages are the same view. The state is a merge.Model, which the GUI's
// view shares.
package mergeview

import (
	"context"
	"errors"
	"strings"

	"github.com/yongjohnlee80/golib/diff"
	"github.com/yongjohnlee80/golib/diff/merge"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// Options configure a MergeView.
type Options struct {
	// Style marks the lines; its zero fields take the defaults.
	Style Style
	// Diff is how lines compare and the diff's bounds.
	Diff diff.Options
	// ConfirmApply asks before applying with changes unresolved; apply applies. nil: the view asks in
	// a dialog of its own.
	ConfirmApply func(changes, conflicts int, apply func())
	// ConfirmReload asks before loading again over a result that was changed; reload loads. nil:
	// the view asks in a dialog of its own.
	ConfirmReload func(reload func())
}

// The panes, by index.
const (
	leftPane = iota
	resultPane
	rightPane
)

// connectorW is each connector column's width: a side's mark, an arrow, ×, the result's mark.
const connectorW = 4

// MergeView merges two texts, with or without their base, into a result.
type MergeView struct {
	core.Base
	src   merge.MergeSource
	opts  Options
	style Style

	model *merge.Model
	panes [3]*editor.Editor
	looks [3][]look // each pane's line looks, by line

	acceptLeft, acceptRight, mergeAll, apply, cancel, reload *core.Button

	loadID, applyID tui.TaskID
	loading         bool
	applying        bool
	failed          error // Load's error, shown in the panes' place
	note            string
	noteErr         bool
	changed         bool // a side changed under the view: a reload is offered
	details         bool
	cur             int  // the change last moved to, or -1
	syncing         bool // panes being scrolled to follow another
	replaying       bool // an operation's edit being put in the result pane
	closed          bool
	onClose         func(applied bool)
	stopWatch       context.CancelFunc // ends the watch of the source's Changed

	// geometry, as last laid out
	paneX, paneW [3]int
	panesY       int
	panesH       int
	h            int
	// barText and barEnd are the columns between the bar's buttons, where its text goes
	barText, barEnd int
}

// NewMergeView is a view of src's merge. It loads once it is mounted.
func NewMergeView(src merge.MergeSource, opts Options) *MergeView {
	v := &MergeView{src: src, opts: opts, style: opts.Style.over(), cur: -1}
	side := func(i int) *editor.Editor {
		return editor.NewEditor(
			editor.WithCore(editor.CoreReadOnly(true), editor.CoreKeymap(leftToTheView(true))),
			editor.WithEditorWrap(core.WrapNone), // a wrapped line would break the panes' rows apart
			editor.WithEditorLineStyle(v.lineStyle(i)),
			editor.WithEditorOnScroll(func(top int) { v.scrolled(i, top) }))
	}
	v.panes[leftPane] = side(leftPane)
	v.panes[rightPane] = side(rightPane)
	v.panes[resultPane] = editor.NewEditor(
		editor.WithCore(editor.CoreOnChange(v.resultChanged), editor.CoreKeymap(leftToTheView(false))),
		editor.WithEditorWrap(core.WrapNone),
		editor.WithEditorLineStyle(v.lineStyle(resultPane)),
		editor.WithEditorOnScroll(func(top int) { v.scrolled(resultPane, top) }))
	button := func(id string, fn func()) *core.Button {
		return core.NewButton("", core.WithLabelMessage(tui.Msg(id)), core.WithOnActivate(fn))
	}
	v.acceptLeft = button("tui.mergeview.acceptLeft", v.AcceptLeft)
	v.acceptRight = button("tui.mergeview.acceptRight", v.AcceptRight)
	v.mergeAll = button("tui.mergeview.merge", v.Merge)
	v.apply = button("tui.mergeview.apply", v.Apply)
	v.cancel = button("tui.button.cancel", v.Cancel)
	v.reload = button("tui.mergeview.reload", v.Reload)
	return v
}

// leftToTheView is the editor keys the view takes instead: in a side pane, every key of its own
// (moving between changes, taking, dismissing, the details); in the result pane, where the others
// are typed, only the moves between changes.
func leftToTheView(side bool) editor.Keymap {
	codes := []rune{'[', ']'}
	if side {
		codes = append(codes, '}', '<', '>', '+', 'x', 'i')
	}
	km := editor.Keymap{}
	for _, mode := range []editor.EditorMode{editor.ModeNormal, editor.ModeVisual} {
		for _, c := range codes {
			km[editor.KeyChord{Mode: mode, Code: c}] = editor.ActUnbound
			if !strings.ContainsRune("[]xi", c) { // typed with Shift on most layouts
				km[editor.KeyChord{Mode: mode, Code: c, Shift: true}] = editor.ActUnbound
			}
		}
	}
	return km
}

// OnClose calls fn once the view is done: applied when Apply wrote the result, false for Cancel.
// Cancel never calls the source's Apply.
func (v *MergeView) OnClose(fn func(applied bool)) { v.onClose = fn }

// Model is the merge's state, nil until it has loaded.
func (v *MergeView) Model() *merge.Model { return v.model }

// Result is the result as text, as Apply would write it ("" before a load).
func (v *MergeView) Result() string {
	if v.model == nil {
		return ""
	}
	return v.model.Result()
}

// Counts are the changes, and of them the conflicts, still unresolved.
func (v *MergeView) Counts() (changes, conflicts int) {
	if v.model == nil {
		return 0, 0
	}
	return v.model.Counts()
}

// Panes are the left, result and right editors.
func (v *MergeView) Panes() (left, result, right *editor.Editor) {
	return v.panes[leftPane], v.panes[resultPane], v.panes[rightPane]
}

// Init mounts the panes and the bar, starts the load, and follows the source's changes.
func (v *MergeView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	for _, p := range v.panes {
		ctx.Mount(p)
	}
	for _, b := range v.buttons() {
		ctx.Mount(b)
	}
	v.load()
	v.watch()
}

// watch follows the source's Changed until the view is unmounted or given another source.
func (v *MergeView) watch() {
	if v.stopWatch != nil {
		v.stopWatch()
		v.stopWatch = nil
	}
	ctx := v.Context()
	if ctx == nil || v.src == nil {
		return
	}
	ch := v.src.Changed()
	if ch == nil {
		return
	}
	wctx, cancel := context.WithCancel(ctx.Ctx())
	v.stopWatch = cancel
	go func() {
		for {
			select {
			case <-wctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				ctx.Update(v.sideChanged)
			}
		}
	}()
}

// SetSource makes src what the view merges, loading it once mounted (at once when it is): a
// declared MergeView is given its source by its host (MergeViewOf).
func (v *MergeView) SetSource(src merge.MergeSource) {
	v.src = src
	v.model, v.failed, v.changed, v.closed = nil, nil, false, false
	if v.Context() != nil {
		v.load()
		v.watch()
	}
}

// SetStyle replaces how lines are marked; zero fields take the defaults.
func (v *MergeView) SetStyle(st Style) {
	v.style = st.over()
	v.refresh()
}

// SetPaneStyles gives the three panes their text's looks, as an Editor's WithStyles does.
func (v *MergeView) SetPaneStyles(st core.TextInputStyles) {
	for _, p := range v.panes {
		p.WithStyles(st)
	}
}

func (v *MergeView) buttons() []*core.Button {
	return []*core.Button{v.acceptLeft, v.acceptRight, v.mergeAll, v.apply, v.cancel, v.reload}
}

// ListChildren is the panes, then the bar's buttons: the order Tab walks.
func (v *MergeView) ListChildren() []tui.Component {
	out := []tui.Component{v.panes[leftPane], v.panes[resultPane], v.panes[rightPane]}
	for _, b := range v.buttons() {
		out = append(out, b)
	}
	return out
}

// InitialFocus is the result pane.
func (v *MergeView) InitialFocus() (tui.Component, bool) { return v.panes[resultPane], true }

// KeyScope and KeyBindings are the view's keys (tui.KeyScoped, tui.KeyBinder).
func (v *MergeView) KeyScope() keymap.Scope { return Scope }

func (v *MergeView) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, commands...)
}

// load asks the source for the texts, off the loop.
func (v *MergeView) load() {
	ctx := v.Context()
	if ctx == nil || v.src == nil {
		return
	}
	v.loading, v.failed = true, nil
	src := v.src // the task reads its own copy: SetSource may change the field meanwhile
	v.loadID = ctx.Go(func(c context.Context) (any, error) { return src.Load(c) }, tui.Exclusive("load"))
	v.MarkDirty()
}

// loaded makes the merge of in and shows it.
func (v *MergeView) loaded(in merge.Input) {
	v.model = merge.NewModel(in, v.opts.Diff)
	v.cur, v.changed = -1, false
	v.panes[leftPane].SetValue(strings.Join(v.model.LeftLines(), "\n"))
	v.panes[rightPane].SetValue(strings.Join(v.model.RightLines(), "\n"))
	v.panes[resultPane].SetValue(strings.Join(v.model.ResultLines(), "\n"))
	v.model.Settle(v.panes[resultPane].Core().Revision())
	v.note, v.noteErr = "", false
	v.refresh()
	v.RequestLayout()
}

// HandleEvent takes the load's and the apply's results, the view's keys and the connectors'
// clicks.
func (v *MergeView) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		switch e.ID {
		case v.loadID:
			v.loading = false
			if e.Err != nil {
				v.failed = e.Err
				v.MarkDirty()
				v.RequestLayout()
				return true
			}
			if in, ok := e.Value.(merge.Input); ok {
				v.loaded(in)
			}
			return true
		case v.applyID:
			v.applying = false
			if e.Err != nil {
				v.note, v.noteErr = kit.WithArg(v.Translate(tui.Msg("tui.mergeview.applyFailed")), e.Err.Error()), true
				v.RequestLayout()
				v.MarkDirty()
				return true
			}
			v.close(true)
			return true
		}
	case tui.KeyEvent:
		if cmd, ok := v.Context().KeyCommand(e, commands...); ok {
			return v.command(cmd)
		}
	case tui.MouseEvent:
		if e.Kind == tui.MousePress && e.Button == tui.MouseLeft {
			return v.click(e.X, e.Y)
		}
	}
	return false
}

// command runs one of the view's keys.
func (v *MergeView) command(cmd keymap.Command) bool {
	if v.model == nil {
		return false
	}
	switch cmd.ID {
	case NextChange.ID:
		v.move(1, false)
	case PrevChange.ID:
		v.move(-1, false)
	case NextConflict.ID:
		v.move(1, true)
	case TakeLeft.ID:
		v.Take(v.current(), merge.Left)
	case TakeRight.ID:
		v.Take(v.current(), merge.Right)
	case TakeBoth.ID:
		v.TakeBoth(v.current())
	case Dismiss.ID:
		v.Dismiss(v.current())
	case Details.ID:
		v.details = !v.details
		v.RequestLayout()
	default:
		return false
	}
	return true
}

// Take resolves change i with side s's lines.
func (v *MergeView) Take(i int, s merge.Side) {
	if v.model == nil {
		return
	}
	if e, ok := v.model.Take(i, s); ok {
		v.cur = i
		v.replay(e)
	}
}

// TakeBoth resolves change i with the left's lines, then the right's.
func (v *MergeView) TakeBoth(i int) {
	if v.model == nil {
		return
	}
	if e, ok := v.model.TakeBoth(i); ok {
		v.cur = i
		v.replay(e)
	}
}

// Dismiss resolves change i as it is.
func (v *MergeView) Dismiss(i int) {
	if v.model != nil && v.model.Dismiss(i) {
		v.cur = i
		v.refresh()
	}
}

// AcceptLeft and AcceptRight make the result that side whole.
func (v *MergeView) AcceptLeft() {
	if v.model != nil {
		v.replay(v.model.AcceptLeft())
	}
}

func (v *MergeView) AcceptRight() {
	if v.model != nil {
		v.replay(v.model.AcceptRight())
	}
}

// Merge resolves every change that is not a conflict with its changed side.
func (v *MergeView) Merge() {
	if v.model == nil {
		return
	}
	if e, ok := v.model.MergeNonConflicting(); ok {
		v.replay(e)
	}
}

// Apply hands the result to the source, asking first when changes are left unresolved. An error
// keeps the view open, said in the bar; success closes it.
func (v *MergeView) Apply() {
	if v.model == nil || v.applying {
		return
	}
	doApply := func() {
		ctx := v.Context()
		if ctx == nil {
			return
		}
		result := v.model.Result()
		v.applying = true
		src := v.src
		v.applyID = ctx.Go(func(c context.Context) (any, error) { return nil, src.Apply(c, result) }, tui.Exclusive("apply"))
	}
	changes, conflicts := v.model.Counts()
	if changes == 0 {
		doApply()
		return
	}
	if v.opts.ConfirmApply != nil {
		v.opts.ConfirmApply(changes, conflicts, doApply)
		return
	}
	v.ask(args(v.Translate(tui.Msg("tui.mergeview.confirmApply")), changes, conflicts), doApply)
}

// Cancel closes the view without applying.
func (v *MergeView) Cancel() { v.close(false) }

func (v *MergeView) close(applied bool) {
	if v.closed {
		return
	}
	v.closed = true
	if v.onClose != nil {
		v.onClose(applied)
	}
}

// Reload loads the texts again, asking first when the result was changed.
func (v *MergeView) Reload() {
	if v.model == nil || !v.model.Touched() {
		v.load()
		return
	}
	if v.opts.ConfirmReload != nil {
		v.opts.ConfirmReload(v.load)
		return
	}
	v.ask(v.Translate(tui.Msg("tui.mergeview.confirmReload")), v.load)
}

// sideChanged offers a reload: a side changed under the view.
func (v *MergeView) sideChanged() {
	v.changed = true
	v.RequestLayout()
	v.MarkDirty()
}

// ask asks a yes-or-no question in a dialog, yes running fn.
func (v *MergeView) ask(question string, fn func()) {
	host, ok := kit.HostFor[core.ModalHost](v.Context())
	if !ok {
		fn() // no host to ask in: the host gave no way to ask, so the answer is the action's
		return
	}
	md := core.NewModal(core.NewText(question),
		core.WithButtons(core.NewStandardButton(core.StandardYes, core.WithOnActivate(fn)), core.NewStandardButton(core.StandardNo)))
	_ = md.OpenOn(host)
}

// args fills a message's %1 and %2.
func args(text string, a, b int) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "%1", itoa(a)), "%2", itoa(b))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// replay puts an operation's edit in the result pane, as one step of its undo, and settles the
// model on the revision it made.
func (v *MergeView) replay(e merge.Edit) {
	c := v.panes[resultPane].Core()
	old := paneLines(c)
	if e.At.Len() > 0 || len(e.Lines) > 0 {
		text := strings.Join(e.Lines, "\n")
		const end = 1 << 30 // a column past a line's end is its end
		var r editor.TextRange
		switch {
		case e.At.To < len(old): // whole lines, a line after them
			r = editor.TextRange{From: editor.TextPos{Line: e.At.From}, To: editor.TextPos{Line: e.At.To}}
			if len(e.Lines) > 0 {
				text += "\n"
			}
		case e.At.From > 0: // to the end, a line before them
			r = editor.TextRange{From: editor.TextPos{Line: e.At.From - 1, Col: end}, To: editor.TextPos{Line: len(old) - 1, Col: end}}
			if len(e.Lines) > 0 {
				text = "\n" + text
			}
		default: // the whole text
			r = editor.TextRange{To: editor.TextPos{Line: max(len(old)-1, 0), Col: end}}
		}
		v.replaying = true
		_, err := c.Replace(r, text)
		v.replaying = false
		if err != nil {
			v.note, v.noteErr = err.Error(), true
		}
	}
	v.model.Settle(c.Revision())
	v.refresh()
}

// paneLines is an editor's text as lines: none for an empty text.
func paneLines(c *editor.EditorCore) []string {
	if c.Value() == "" {
		return []string{}
	}
	return c.Lines()
}

// resultChanged takes a change to the result pane that the view did not make: a hand edit, an
// undo, a redo.
func (v *MergeView) resultChanged() {
	if v.replaying || v.model == nil {
		return
	}
	c := v.panes[resultPane].Core()
	v.model.Edited(paneLines(c), c.Revision())
	v.refresh()
}

// refresh recomputes each pane's line looks and the bar after the model changed.
func (v *MergeView) refresh() {
	v.computeLooks()
	if v.model != nil {
		has := false
		for _, c := range v.model.Changes() {
			if c.State == merge.Unresolved && !c.Conflict() {
				has = true
				break
			}
		}
		v.mergeAll.SetEnabled(has)
	}
	for _, p := range v.panes {
		p.MarkDirty()
	}
	v.MarkDirty()
	v.RequestLayout()
}

// errNotText reports whether err is the source's "not text".
func errNotText(err error) bool { return errors.Is(err, merge.ErrNotText) }

// header is a pane's header text: its origin's kind, title and short revision.
func (v *MergeView) header(pane int) string {
	if v.model == nil {
		return ""
	}
	in := v.model.Input()
	o := [3]merge.Origin{in.LeftFrom, in.ResultTo, in.RightFrom}[pane]
	parts := []string{v.Translate(tui.Msg("tui.mergeview.kind." + o.Kind.String()))}
	if pane == resultPane {
		parts = []string{v.Translate(tui.Msg("tui.mergeview.result")), "→", parts[0]}
	}
	if t := o.Title(); t != "" {
		parts = append(parts, t)
	}
	if r := o.ShortRevision(); r != "" && r != o.Title() {
		parts = append(parts, "@"+r)
	}
	return strings.Join(parts, " ")
}

// detail is a pane's details row: its origin's full location and revision.
func (v *MergeView) detail(pane int) string {
	in := v.model.Input()
	o := [3]merge.Origin{in.LeftFrom, in.ResultTo, in.RightFrom}[pane]
	return strings.TrimSpace(o.Location + " " + o.Revision)
}

// Layout lays the headers, the panes between the connectors, and the bar.
func (v *MergeView) Layout(c tui.Constraints) tui.Size {
	ctx := v.Context()
	w, h := c.MaxW, c.MaxH
	v.h = h
	v.panesY = 1
	if v.details {
		v.panesY = 2
	}
	v.panesH = max(h-v.panesY-1, 0)
	text := max(w-2*connectorW, 3)
	side := text / 3
	v.paneW = [3]int{side, text - 2*side, side}
	v.paneX = [3]int{0, side + connectorW, side + connectorW + (text - 2*side) + connectorW}
	shown := v.model != nil && v.failed == nil
	for i, p := range v.panes {
		r := tui.Rect{X: v.paneX[i], Y: v.panesY, W: v.paneW[i], H: v.panesH}
		if !shown {
			r = tui.Rect{}
		}
		ctx.LayoutChild(p, tui.Tight(tui.Size{W: r.W, H: r.H}))
		ctx.PlaceChild(p, r)
	}
	// The bar: Accept Left, Accept Right, Merge at the left; Reload (when offered), Apply and Cancel
	// at the right; the counts and the note between.
	y := h - 1
	x := 0
	place := func(b *core.Button, show bool) int {
		if !show {
			ctx.LayoutChild(b, tui.Tight(tui.Size{}))
			ctx.PlaceChild(b, tui.Rect{})
			return 0
		}
		sz := ctx.LayoutChild(b, tui.Loose(tui.Size{W: w, H: 1}))
		return sz.W
	}
	for _, b := range []*core.Button{v.acceptLeft, v.acceptRight, v.mergeAll} {
		bw := place(b, shown)
		if bw > 0 {
			ctx.PlaceChild(b, tui.Rect{X: x, Y: y, W: bw, H: 1})
			x += bw + 1
		}
	}
	v.barText = x
	right := w
	for _, b := range []*core.Button{v.cancel, v.apply, v.reload} {
		bw := place(b, b == v.cancel || (shown && (b == v.apply || v.changed)))
		if bw > 0 {
			right -= bw
			ctx.PlaceChild(b, tui.Rect{X: right, Y: y, W: bw, H: 1})
			right--
		}
	}
	v.barEnd = right
	return tui.Size{W: w, H: h}
}

// Render draws the headers, the details, the connectors, the bar's text, or what kept the merge
// from being shown.
func (v *MergeView) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", style.New())
	head := style.New().Bold(true)
	muted := style.New().Foreground(style.TokenTextMuted)
	switch {
	case v.failed != nil:
		msg := kit.WithArg(v.Translate(tui.Msg("tui.mergeview.loadFailed")), v.failed.Error())
		if errNotText(v.failed) {
			msg = v.Translate(tui.Msg("tui.mergeview.notText"))
		}
		tui.DrawText(s, 1, 1, msg, style.New().Foreground(style.TokenError))
	case v.model == nil:
		tui.DrawText(s, 1, 1, v.Translate(tui.Msg("tui.mergeview.loading")), muted)
	default:
		for i := range v.panes {
			v.clipText(s, v.paneX[i], 0, v.paneW[i], v.header(i), head)
			if v.details {
				v.clipText(s, v.paneX[i], 1, v.paneW[i], v.detail(i), muted)
			}
		}
		for _, g := range v.glyphs() {
			s.SetCell(g.x, g.y, g.text, g.st)
		}
	}
	v.renderBar(s)
}

// clipText draws text from x, no wider than w.
func (v *MergeView) clipText(s tui.Surface, x, y, w int, text string, st style.Style) {
	if w <= 0 {
		return
	}
	sub := s.Sub(tui.Rect{X: x, Y: y, W: w, H: 1})
	tui.DrawText(sub, 0, 0, text, st)
}

// renderBar draws the counts and the note between the bar's buttons.
func (v *MergeView) renderBar(s tui.Surface) {
	if v.h <= 0 || v.barEnd <= v.barText {
		return
	}
	var parts []string
	st := style.New().Foreground(style.TokenTextMuted)
	// What needs the reader first: an Apply's error, a side changed; then the counts.
	if v.note != "" {
		parts = append(parts, v.note)
		if v.noteErr {
			st = style.New().Foreground(style.TokenError)
		}
	}
	if v.changed {
		parts = append(parts, v.Translate(tui.Msg("tui.mergeview.changed")))
	}
	if v.model != nil && v.failed == nil {
		changes, conflicts := v.model.Counts()
		if changes == 0 {
			parts = append(parts, v.Translate(tui.Msg("tui.mergeview.resolved")))
		} else {
			parts = append(parts, args(v.Translate(tui.Msg("tui.mergeview.counts")), changes, conflicts))
		}
		if v.model.Coarse() {
			parts = append(parts, v.Translate(tui.Msg("tui.mergeview.coarse")))
		}
	}
	v.clipText(s, v.barText, v.h-1, v.barEnd-v.barText, strings.Join(parts, " · "), st)
}
