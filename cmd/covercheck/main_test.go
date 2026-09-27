package main

import (
	"bytes"
	"context"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/covercheck"
)

func TestRunExitContract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "a.go", "package fixture\nfunc value() int { return 1 }\n")
	base := writeFixture(t, root, "base.out", "mode: set\nexample.com/p/a.go:1.1,2.2 1 1\n")
	head := writeFixture(t, root, "head.out", "mode: set\nexample.com/p/a.go:1.1,2.2 1 0\n")
	diff := writeFixture(t, root, "change.diff", "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "pass", args: []string{"--module", "example.com/p", "--root", root, "--base-profile", base, "--head-profile", head, "--diff", diff}, want: exitPass},
		{name: "policy", args: []string{"--module", "example.com/p", "--root", root, "--base-profile", base, "--head-profile", head, "--diff", diff, "--maximum-file-regression", "0"}, want: exitPolicy},
		{name: "operational", args: []string{"--module", "example.com/p"}, want: exitOperational},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got := run(context.Background(), test.args, &stdout, &stderr); got != test.want {
				t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, test.want, stdout.String(), stderr.String())
			}
		})
	}
}

// TestRunRequireChangedFilesIsBuildAware: a changed file the running platform cannot build is reported,
// not a missing-file violation; a buildable file absent from the profile still is.
func TestRunRequireChangedFilesIsBuildAware(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "a.go", "package fixture\nfunc value() int { return 1 }\n")
	writeFixture(t, root, "sys_plan9.go", "package fixture\nfunc sys() int { return 2 }\n")
	writeFixture(t, root, "b.go", "package fixture\nfunc other() int { return 3 }\n")
	base := writeFixture(t, root, "base.out", "mode: set\nexample.com/p/a.go:1.1,2.2 1 1\n")
	head := writeFixture(t, root, "head.out", "mode: set\nexample.com/p/a.go:1.1,2.2 1 1\n")
	plan9 := "diff --git a/sys_plan9.go b/sys_plan9.go\nnew file mode 100644\n--- /dev/null\n+++ b/sys_plan9.go\n@@ -0,0 +1,2 @@\n+package fixture\n+func sys() int { return 2 }\n"
	other := "diff --git a/b.go b/b.go\nnew file mode 100644\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1,2 @@\n+package fixture\n+func other() int { return 3 }\n"
	for name, c := range map[string]struct {
		diff string
		want int
	}{
		"excluded file passes":         {plan9, exitPass},
		"buildable missing file fails": {plan9 + other, exitPolicy},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diff := writeFixture(t, t.TempDir(), "change.diff", c.diff)
			var stdout, stderr bytes.Buffer
			got := run(context.Background(), []string{"--module", "example.com/p", "--root", root, "--base-profile", base,
				"--head-profile", head, "--diff", diff, "--require-changed-files"}, &stdout, &stderr)
			if got != c.want {
				t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, c.want, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "sys_plan9.go") || !strings.Contains(stdout.String(), "not built on") {
				t.Fatalf("the excluded file is not reported:\n%s", stdout.String())
			}
		})
	}
}

func TestRunJSONMatchesPolicy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "a.go", "package fixture\nfunc value() int { return 1 }\n")
	base := writeFixture(t, root, "base.out", "mode: count\nexample.com/p/a.go:1.1,2.2 1 1\n")
	head := writeFixture(t, root, "head.out", "mode: count\nexample.com/p/a.go:1.1,2.2 1 1\n")
	diff := writeFixture(t, root, "change.diff", "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n")
	var stdout, stderr bytes.Buffer
	got := run(context.Background(), []string{"--module", "example.com/p", "--root", root, "--base-profile", base, "--head-profile", head, "--diff", diff, "--format", "json"}, &stdout, &stderr)
	if got != exitPass {
		t.Fatalf("exit = %d; stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"passed": true`) {
		t.Fatalf("JSON result = %s", stdout.String())
	}
}

func TestProductionGoChanges(t *testing.T) {
	t.Parallel()
	changes := productionGoChanges([]covercheck.FileChange{
		{NewPath: "a.go"},
		{NewPath: "a_test.go"},
		{NewPath: "README.md"},
		{OldPath: "deleted.go"},
	})
	if len(changes) != 2 || changes[0].NewPath != "a.go" || changes[1].OldPath != "deleted.go" {
		t.Fatalf("filtered changes = %#v", changes)
	}
}

func TestAnnotateExecutability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "doc.go", "// Package fixture documents a fixture.\npackage fixture\n")
	writeFixture(t, root, "run.go", "package fixture\nfunc run() { println(1) }\n")
	changes := []covercheck.FileChange{
		{NewPath: "doc.go", Kind: covercheck.ChangeAdded},
		{NewPath: "run.go", Kind: covercheck.ChangeAdded},
	}
	if err := annotateExecutability(root, changes); err != nil {
		t.Fatal(err)
	}
	if changes[0].Executability != covercheck.ExecutabilityAbsent {
		t.Fatalf("doc.go = %q, want absent", changes[0].Executability)
	}
	if changes[1].Executability != covercheck.ExecutabilityPresent {
		t.Fatalf("run.go = %q, want present", changes[1].Executability)
	}
}

func TestAnnotateBuild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "all.go", "package fixture\nfunc all() { println(1) }\n")
	writeFixture(t, root, "only_darwin.go", "package fixture\nfunc d() { println(1) }\n")
	writeFixture(t, root, "tagged.go", "//go:build darwin\n\npackage fixture\nfunc t() { println(1) }\n")
	writeFixture(t, root, "unix.go", "//go:build unix\n\npackage fixture\nfunc u() { println(1) }\n")
	changes := []covercheck.FileChange{
		{NewPath: "all.go", Kind: covercheck.ChangeAdded},
		{NewPath: "only_darwin.go", Kind: covercheck.ChangeAdded},
		{NewPath: "tagged.go", Kind: covercheck.ChangeModified},
		{NewPath: "unix.go", Kind: covercheck.ChangeAdded},
		{OldPath: "gone_darwin.go", Kind: covercheck.ChangeDeleted},
	}
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH = "linux", "amd64"
	if err := annotateBuild(root, changes, ctxt); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"", "linux/amd64", "linux/amd64", "", ""} {
		if changes[i].ExcludedOn != want {
			t.Errorf("%s%s: ExcludedOn = %q, want %q", changes[i].NewPath, changes[i].OldPath, changes[i].ExcludedOn, want)
		}
	}
	ctxt.GOOS = "darwin"
	for i := range changes {
		changes[i].ExcludedOn = ""
	}
	if err := annotateBuild(root, changes, ctxt); err != nil {
		t.Fatal(err)
	}
	for i := range changes {
		if changes[i].ExcludedOn != "" {
			t.Errorf("on darwin %s: ExcludedOn = %q, want built", changes[i].NewPath, changes[i].ExcludedOn)
		}
	}
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	filename := filepath.Join(dir, name)
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}
