package covercheck

import (
	"strings"
	"testing"
)

func TestParseUnifiedDiff(t *testing.T) {
	t.Parallel()
	diff := `diff --git a/pkg/a.go b/pkg/a.go
--- a/pkg/a.go
+++ b/pkg/a.go
@@ -2,0 +3,2 @@
+one
+two
@@ -10 +12 @@
-old
+new
diff --git a/old name.go b/new name.go
similarity index 90%
rename from old name.go
rename to new name.go
--- a/old name.go
+++ b/new name.go
@@ -5 +5 @@
-old
+new
diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-gone
`
	changes, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want 3", len(changes))
	}
	if got := changes[0].Ranges; len(got) != 2 || got[0] != (LineRange{Start: 3, End: 4}) || got[1] != (LineRange{Start: 12, End: 12}) {
		t.Fatalf("ranges = %#v", got)
	}
	if changes[1].Kind != ChangeRenamed || changes[1].OldPath != "old name.go" || changes[1].NewPath != "new name.go" {
		t.Fatalf("rename = %#v", changes[1])
	}
	if changes[2].Kind != ChangeDeleted || changes[2].NewPath != "" || len(changes[2].Ranges) != 0 {
		t.Fatalf("deletion = %#v", changes[2])
	}
}

func TestParseUnifiedDiffQuotedPath(t *testing.T) {
	t.Parallel()
	diff := "diff --git \"a/pkg/\\303\\251.go\" \"b/pkg/\\303\\251.go\"\n" +
		"--- \"a/pkg/\\303\\251.go\"\n" +
		"+++ \"b/pkg/\\303\\251.go\"\n" +
		"@@ -1 +1 @@\n-old\n+new\n"
	changes, err := ParseUnifiedDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].NewPath != "pkg/é.go" {
		t.Fatalf("changes = %#v", changes)
	}
}
