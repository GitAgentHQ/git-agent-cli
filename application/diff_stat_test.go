package application

import "testing"

func TestParseDiffStat(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index 111..222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
-old line
+new line 1
+new line 2
 trailing
diff --git a/util.go b/util.go
new file mode 100644
index 000..333
--- /dev/null
+++ b/util.go
@@ -0,0 +1,2 @@
+package util
+// helper`

	stats := parseDiffStat(diff)

	if got := stats["main.go"]; got.Additions != 2 || got.Deletions != 1 {
		t.Errorf("main.go = +%d/-%d, want +2/-1", got.Additions, got.Deletions)
	}
	if got := stats["util.go"]; got.Additions != 2 || got.Deletions != 0 {
		t.Errorf("util.go = +%d/-%d, want +2/-0", got.Additions, got.Deletions)
	}
}

func TestParseDiffStat_Deletion(t *testing.T) {
	diff := `diff --git a/gone.go b/gone.go
deleted file mode 100644
index 444..000
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
-// removed`

	got := parseDiffStat(diff)["gone.go"]
	if got.Additions != 0 || got.Deletions != 2 {
		t.Errorf("gone.go = +%d/-%d, want +0/-2", got.Additions, got.Deletions)
	}
}

func TestParseDiffStat_Empty(t *testing.T) {
	if got := parseDiffStat(""); len(got) != 0 {
		t.Errorf("empty diff produced %d entries, want 0", len(got))
	}
}
