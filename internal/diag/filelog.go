// SPDX-License-Identifier: GPL-3.0-or-later

package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RotatingFile is an append-only log file that moves to path.1, path.2, …
// when it grows past maxSize bytes; keep files in all are retained.
type RotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

// OpenRotating opens (creating the directory and file) path for appending.
func OpenRotating(path string, maxSize int64, keep int) (*RotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	r := &RotatingFile{path: path, max: maxSize, keep: max(keep, 1)}
	return r, r.open()
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// The log is masked by Redact, so any local user may read it
	// ("tuntop logs" runs without root).
	_ = f.Chmod(0o644)
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

// Write implements io.Writer.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) rotate() error {
	_ = r.f.Close()
	for i := r.keep - 1; i >= 1; i-- {
		from := r.path
		if i > 1 {
			from = fmt.Sprintf("%s.%d", r.path, i-1)
		}
		_ = os.Rename(from, fmt.Sprintf("%s.%d", r.path, i))
	}
	if r.keep == 1 {
		_ = os.Remove(r.path)
	}
	return r.open()
}

// Close closes the file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// Stamped writes each line of its input to W with a timestamp, masked by
// Redact. Use it as a log.Logger output (flags 0).
type Stamped struct {
	W   interface{ Write([]byte) (int, error) }
	Now func() time.Time // nil: time.Now
}

// Write implements io.Writer; it reports len(p) so log never sees a short write.
func (s Stamped) Write(p []byte) (int, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	ts := now().Format("2006-01-02T15:04:05.000Z07:00")
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		b.WriteString(ts + " " + Redact(l) + "\n")
	}
	if _, err := s.W.Write([]byte(b.String())); err != nil {
		return 0, err
	}
	return len(p), nil
}
