//go:build linux

package gitbackend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileWatcherDetectsFileWriteSymlinkAndDirectoryRefUpdate(t *testing.T) {
	tmp := t.TempDir()

	// 1. Symlinked log file (mirrors /var/log/srlinux/buffer/messages -> ramdisk/messages)
	realDir := filepath.Join(tmp, "ramdisk")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	realLog := filepath.Join(realDir, "messages")
	if err := os.WriteFile(realLog, []byte("boot\n"), 0644); err != nil {
		t.Fatal(err)
	}
	symLog := filepath.Join(tmp, "messages")
	if err := os.Symlink(realLog, symLog); err != nil {
		t.Fatal(err)
	}

	// 2. Directory watch (mirrors .git/refs/heads)
	refsDir := filepath.Join(tmp, "refs", "heads")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatal(err)
	}

	w, err := NewFileWatcher(symLog, refsDir)
	if err != nil {
		t.Fatalf("NewFileWatcher failed: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Trigger write to real symlink target -> event must fire immediately (<250ms).
	f, err := os.OpenFile(realLog, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("EV|commitSucceeded|I: All changes have been committed successfully\n")
	_ = f.Close()

	select {
	case got := <-w.Events:
		if filepath.Base(got) != "messages" {
			t.Fatalf("expected messages event, got %q", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for symlinked log write event")
	}

	// Trigger ref write inside watched directory -> event must fire immediately
	// (drain any trailing IN_CLOSE_WRITE from the previous messages append).
	refPath := filepath.Join(refsDir, "main")
	if err := os.WriteFile(refPath, []byte("0123456789abcdef0123456789abcdef01234567\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(500 * time.Millisecond)
	gotMain := false
	for !gotMain {
		select {
		case got := <-w.Events:
			if filepath.Base(got) == "main" {
				gotMain = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for ref directory write event")
		}
	}

	// Close must return cleanly and be idempotent.
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
