package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// signalparams_test.go covers signal PARAMETERS: a handler passing on what the
// signal was raised with, as QML's do — `onAccepted: App.openFile(selectedFile)`.

// paramAdapter declares Picker's `chosen(path, count)`.
type paramAdapter struct{ *recorder }

func (paramAdapter) SignalParams(typeName, signal string) []string {
	if typeName == "Picker" && signal == "chosen" {
		return []string{"path", "count"}
	}
	return nil
}

func paramTree(t *testing.T, src string, inject ...string) (*decl.Tree, *recorder, *[][]string, error) {
	t.Helper()
	a := paramAdapter{newRecorder()}
	tr := decl.New(a)
	calls := &[][]string{}
	if err := tr.Inject("pick", decl.Handle(func(args []qml.SpecValue) error {
		var got []string
		for _, v := range args {
			got = append(got, v.Raw)
		}
		*calls = append(*calls, got)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	for _, n := range inject {
		if err := tr.Inject(n, decl.Constant(sv("injected-"+n))); err != nil {
			t.Fatal(err)
		}
	}
	return tr, a.recorder, calls, tr.Mount(qmlDoc(t, src))
}

func TestAHandlerPassesOnTheSignalsParameters(t *testing.T) {
	tr, rec, calls, err := paramTree(t, `Picker { onChosen: pick(count, "lit", path) }`)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := rec.emitters[tr.Root()]["chosen"](sv("/tmp/a.txt"), sv("3")); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if got := strings.Join((*calls)[0], ","); got != "3,lit,/tmp/a.txt" {
		t.Fatalf("pick got %q, want the parameters where the handler named them", got)
	}
}

// TestAParameterShadowsAnInjectedName: the handler is the innermost scope.
func TestAParameterShadowsAnInjectedName(t *testing.T) {
	tr, rec, calls, err := paramTree(t, `Picker { onChosen: pick(path) }`, "path")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	_ = rec.emitters[tr.Root()]["chosen"](sv("from-signal"), sv("1"))
	if got := (*calls)[0][0]; got != "from-signal" {
		t.Fatalf("pick got %q, want the parameter over the injected name", got)
	}
}

// TestAParameterBelongsToItsSignal: another signal, or another type, has no
// such name, and saying so at mount beats an empty value at the first press.
func TestAParameterBelongsToItsSignal(t *testing.T) {
	for _, src := range []string{
		`Picker { onOther: pick(path) }`,
		`Box { onChosen: pick(path) }`,
		`Picker { onChosen: pick(path.x) }`,
	} {
		if _, _, _, err := paramTree(t, src); err == nil {
			t.Errorf("%s: mounted, naming a parameter that signal does not have", src)
		}
	}
}

// TestASignalRaisedWithoutItsParameterSaysSo rather than passing nothing.
func TestASignalRaisedWithoutItsParameterSaysSo(t *testing.T) {
	tr, rec, calls, err := paramTree(t, `Picker { onChosen: pick(count) }`)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	err = rec.emitters[tr.Root()]["chosen"](sv("only-path"))
	if err == nil || !strings.Contains(err.Error(), `parameter "count"`) || len(*calls) != 0 {
		t.Fatalf("err = %v, calls = %v; want the missing parameter named and nothing run", err, *calls)
	}
}

// TestADeferredSignalDeliversItsLatestParameters: raised twice mid-mount, it
// is ONE delivery, carrying what the second raise said.
func TestADeferredSignalDeliversItsLatestParameters(t *testing.T) {
	a := paramAdapter{newRecorder()}
	var got []string
	tr := decl.New(a)
	if err := tr.Inject("pick", decl.Handle(func(args []qml.SpecValue) error {
		got = append(got, args[0].Raw)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	a.recorder.onApply = func(app decl.Application) {
		em := a.recorder.emitters[app.Node]["chosen"]
		_ = em(sv("first"), sv("1"))
		_ = em(sv("second"), sv("2"))
	}
	if err := tr.Mount(qmlDoc(t, `Picker { title: "x"; onChosen: pick(path) }`)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if strings.Join(got, ",") != "second" {
		t.Fatalf("delivered %v, want one delivery with the latest parameters", got)
	}
}
