// Package logbuffer persists per-container stdout/stderr lines to disk
// for the configured retention window (Phase 6 default: 6 hours) and
// republishes each line on the realtime hub for live subscribers.
//
// Storage shape: one NDJSON file per container, named by container ID,
// under <root>/<id>.ndjson. Each line:
//
//	{"t": "<RFC3339Nano>", "s": "stdout|stderr", "l": "<text>"}
//
// Disk format chosen over SQLite: log lines arrive at high velocity and
// the only access patterns are append + tail-from-offset + prune-by-time.
// Append-to-file + a periodic linear-scan rewrite is dirt-simple and
// avoids the WAL flush per line that an INSERT would incur.
package logbuffer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Line is the public shape of one persisted log line. Matches the
// on-disk JSON exactly so HTTP handlers can pass it through.
type Line struct {
	Timestamp time.Time `json:"t"`
	Stream    string    `json:"s"` // "stdout" | "stderr"
	Line      string    `json:"l"`
}

// Buffer is one container's append-only log file. Methods are safe for
// concurrent use; the mutex serialises Append vs Tail vs Prune so a
// pruning rewrite can't corrupt a concurrent reader.
type Buffer struct {
	containerID string
	path        string

	mu sync.Mutex
}

// NewBuffer constructs a Buffer rooted at root/<containerID>.ndjson.
// The parent directory is created lazily on the first Append.
func NewBuffer(root, containerID string) *Buffer {
	return &Buffer{
		containerID: containerID,
		path:        filepath.Join(root, containerID+".ndjson"),
	}
}

// Path returns the on-disk file path. Exposed for tests + the API
// handler that streams the file body for historical reads.
func (b *Buffer) Path() string { return b.path }

// Append writes one line to the file. Errors are returned but the
// caller (the tailer goroutine) typically just logs them — losing one
// log line is preferable to crashing the tailer.
func (b *Buffer) Append(l Line) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return fmt.Errorf("logbuffer: mkdir: %w", err)
	}
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("logbuffer: open: %w", err)
	}
	defer f.Close()

	body, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("logbuffer: marshal: %w", err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("logbuffer: write: %w", err)
	}
	return nil
}

// Tail returns up to limit lines whose timestamp is at or after `since`.
// `limit <= 0` returns every matching line. Returned lines are in file
// (chronological) order.
func (b *Buffer) Tail(since time.Time, limit int) ([]Line, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.Open(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("logbuffer: open: %w", err)
	}
	defer f.Close()

	out := make([]Line, 0, 64)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue // skip malformed lines (extremely unlikely; an interrupted append could leave half a row)
		}
		if !since.IsZero() && l.Timestamp.Before(since) {
			continue
		}
		out = append(out, l)
		if limit > 0 && len(out) >= limit {
			// We're collecting from oldest to newest; if `limit` is hit
			// keep going so the final window is the *latest* `limit`,
			// not the earliest. Implementation: just collect all then
			// trim. Simpler than back-seeking.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("logbuffer: scan: %w", err)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Prune drops lines whose timestamp is before `cutoff`. Implemented as
// a tail-rewrite: read the file, write surviving lines to a sibling
// temp file, rename. Atomic from the reader's perspective. Returns the
// number of lines dropped.
func (b *Buffer) Prune(cutoff time.Time) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	src, err := os.Open(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(b.path), filepath.Base(b.path)+".prune.*")
	if err != nil {
		return 0, fmt.Errorf("logbuffer: temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		// On any exit path, ensure no stray temp file remains.
		_ = os.Remove(tmpPath)
	}()

	w := bufio.NewWriter(tmp)
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	dropped := 0
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			// Preserve malformed lines verbatim — better than data loss.
			_, _ = w.Write(sc.Bytes())
			_ = w.WriteByte('\n')
			continue
		}
		if l.Timestamp.Before(cutoff) {
			dropped++
			continue
		}
		_, _ = w.Write(sc.Bytes())
		_ = w.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if dropped == 0 {
		return 0, nil // nothing to do; leave original alone
	}
	if err := os.Rename(tmpPath, b.path); err != nil {
		return 0, fmt.Errorf("logbuffer: rename: %w", err)
	}
	return dropped, nil
}

// Delete removes the on-disk file. Used when a container hasn't existed
// for longer than the retention window — its buffer is no longer
// useful.
func (b *Buffer) Delete() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := os.Remove(b.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Reader returns an io.ReadCloser of the raw NDJSON file. Caller must
// Close it. Used by the API endpoint that streams persisted lines for
// download/grep.
func (b *Buffer) Reader() (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, err := os.Open(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return io.NopCloser(nil), nil
		}
		return nil, err
	}
	return f, nil
}
