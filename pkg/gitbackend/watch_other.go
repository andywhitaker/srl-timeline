//go:build !linux

package gitbackend

import "errors"

// FileWatcher is a no-op stub on non-Linux platforms (callers fall back to polling).
type FileWatcher struct {
	Events <-chan string
}

// NewFileWatcher returns an unsupported error on non-Linux platforms.
func NewFileWatcher(_ ...string) (*FileWatcher, error) {
	return nil, errors.New("inotify file watcher is only supported on Linux")
}

// Close is a no-op on non-Linux platforms.
func (w *FileWatcher) Close() error {
	return nil
}
