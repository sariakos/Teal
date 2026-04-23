package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkdirCreateAndPath(t *testing.T) {
	root := t.TempDir()
	w := NewWorkdirs(root)

	p, err := w.Create("myapp", 7)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p != filepath.Join(root, "deploys", "myapp", "7") {
		t.Errorf("Path mismatch: %s", p)
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}

func TestWorkdirPruneKeepsRecent(t *testing.T) {
	root := t.TempDir()
	w := NewWorkdirs(root)
	for _, id := range []int64{1, 2, 3, 4, 5} {
		if _, err := w.Create("myapp", id); err != nil {
			t.Fatal(err)
		}
	}
	n, err := w.Prune("myapp", 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 3 {
		t.Errorf("Prune removed %d, want 3", n)
	}
	// 4 and 5 should remain.
	for _, id := range []int64{4, 5} {
		if _, err := os.Stat(w.Path("myapp", id)); err != nil {
			t.Errorf("expected dir %d to remain: %v", id, err)
		}
	}
	for _, id := range []int64{1, 2, 3} {
		if _, err := os.Stat(w.Path("myapp", id)); err == nil {
			t.Errorf("expected dir %d to be removed", id)
		}
	}
}

func TestWorkdirPruneIgnoresNonNumericEntries(t *testing.T) {
	root := t.TempDir()
	w := NewWorkdirs(root)
	_, _ = w.Create("myapp", 1)
	if err := os.WriteFile(filepath.Join(w.AppPath("myapp"), "scratch"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Prune("myapp", 0); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// scratch file must still exist.
	if _, err := os.Stat(filepath.Join(w.AppPath("myapp"), "scratch")); err != nil {
		t.Errorf("scratch file should be preserved: %v", err)
	}
}

func TestWorkdirRemoveAppRefusesPathTraversal(t *testing.T) {
	root := t.TempDir()
	w := NewWorkdirs(root)
	if err := w.RemoveApp("../etc"); err == nil {
		t.Error("RemoveApp should refuse slugs containing path separators")
	}
}
