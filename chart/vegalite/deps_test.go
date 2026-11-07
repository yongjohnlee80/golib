package vegalite

import (
	"os/exec"
	"strings"
	"testing"
)

// The chart package must stay gio-free: a build without the GUI (a terminal's, a headless
// server's) lays out and writes a chart with it, and this is the proof, over the packages it
// actually compiles in, not only the module it lives in. Neither Gio (either import path) nor
// golib's gui module may be among them; gui/vegalite paints over this package, never the reverse.
func TestTheChartPackageStaysGioFree(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps .: %v", err)
	}
	deps := strings.Fields(string(out))
	// the list is the real one: the reader it plots with is in it, so an empty or wrong answer
	// can't pass the guard by saying nothing
	if !containsDep(deps, "github.com/yongjohnlee80/golib/parse/vegalite") {
		t.Fatalf("go list -deps . doesn't list parse/vegalite: the guard isn't looking at this package (%d deps)", len(deps))
	}
	for _, dep := range deps {
		for _, banned := range []string{"gioui.org", "github.com/yongjohnlee80/gio", "github.com/yongjohnlee80/golib/gui"} {
			if dep == banned || strings.HasPrefix(dep, banned+"/") {
				t.Errorf("the chart package depends on %s: it must stay gio-free, so a build without the GUI draws charts", dep)
			}
		}
	}
}

func containsDep(deps []string, want string) bool {
	for _, d := range deps {
		if d == want {
			return true
		}
	}
	return false
}
