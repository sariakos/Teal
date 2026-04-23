package logbuffer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndTail(t *testing.T) {
	dir := t.TempDir()
	b := NewBuffer(dir, "abc")

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = b.Append(Line{Timestamp: now.Add(time.Duration(i) * time.Second), Stream: "stdout", Line: "line"})
	}

	all, err := b.Tail(time.Time{}, 0)
	if err != nil {
		t.Fatalf("Tail all: %v", err)
	}
	if len(all) != 5 {
		t.Errorf("Tail all len: %d", len(all))
	}

	// Limit returns the LATEST `limit` lines.
	last2, _ := b.Tail(time.Time{}, 2)
	if len(last2) != 2 || !last2[0].Timestamp.Equal(now.Add(3*time.Second)) {
		t.Errorf("limit: %+v", last2)
	}

	// Since cutoff filters older lines.
	since3, _ := b.Tail(now.Add(3*time.Second), 0)
	if len(since3) != 2 {
		t.Errorf("since: %d", len(since3))
	}
}

func TestTailMissingFileReturnsEmpty(t *testing.T) {
	b := NewBuffer(t.TempDir(), "missing")
	rows, err := b.Tail(time.Time{}, 0)
	if err != nil {
		t.Fatalf("Tail missing: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty, got %+v", rows)
	}
}

func TestPruneRewritesFile(t *testing.T) {
	dir := t.TempDir()
	b := NewBuffer(dir, "abc")

	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		_ = b.Append(Line{Timestamp: now.Add(time.Duration(i) * time.Minute), Stream: "stdout", Line: "x"})
	}
	dropped, err := b.Prune(now.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if dropped != 3 {
		t.Errorf("dropped: %d (want 3)", dropped)
	}
	rows, _ := b.Tail(time.Time{}, 0)
	if len(rows) != 3 {
		t.Errorf("post-prune: %d", len(rows))
	}
}

func TestPruneNoOpWhenNothingDropped(t *testing.T) {
	dir := t.TempDir()
	b := NewBuffer(dir, "abc")
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		_ = b.Append(Line{Timestamp: now.Add(time.Duration(i) * time.Minute), Stream: "stdout", Line: "x"})
	}
	dropped, err := b.Prune(now.Add(-1 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Errorf("dropped: %d", dropped)
	}
	// File should still be readable.
	rows, _ := b.Tail(time.Time{}, 0)
	if len(rows) != 3 {
		t.Errorf("post-noop: %d", len(rows))
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	b := NewBuffer(dir, "abc")
	_ = b.Append(Line{Timestamp: time.Now(), Stream: "stdout", Line: "x"})
	if err := b.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Tail(time.Time{}, 0); err != nil {
		t.Errorf("Tail after delete: %v", err)
	}
	// Path should now not exist.
	if _, err := filepath.Glob(b.Path()); err != nil {
		t.Errorf("Glob: %v", err)
	}
}
