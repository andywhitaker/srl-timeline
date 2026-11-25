//go:build linux

package gitbackend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// FileWatcher subscribes to Linux kernel inotify(7) filesystem events via epoll(7)
// with zero polling and zero external dependencies. It watches parent directories for
// file targets so atomic renames and log rotations are handled transparently.
type FileWatcher struct {
	Events <-chan string

	wakeW     *os.File
	eventsCh  chan string
	doneCh    chan struct{}
	closeOnce sync.Once
}

type watchTarget struct {
	dir        string
	watchAll   bool
	matchNames map[string]string // base filename -> resolved path
}

// NewFileWatcher starts a kernel inotify+epoll watcher for the given file or directory paths.
// Symlinks are resolved to their real target so watching symlinks like
// /var/log/srlinux/buffer/messages observes writes to ramdisk/messages directly.
func NewFileWatcher(paths ...string) (*FileWatcher, error) {
	inotifyFd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}

	// Group paths by resolved parent directory (or directory itself).
	byDir := make(map[string]*watchTarget)
	for _, rawPath := range paths {
		if strings.TrimSpace(rawPath) == "" {
			continue
		}
		resolved := resolveWatchPath(rawPath)
		info, statErr := os.Stat(resolved)
		if statErr == nil && info.IsDir() {
			t, ok := byDir[resolved]
			if !ok {
				t = &watchTarget{dir: resolved, matchNames: make(map[string]string)}
				byDir[resolved] = t
			}
			t.watchAll = true
			continue
		}

		dir := filepath.Dir(resolved)
		base := filepath.Base(resolved)
		if dir == "" || base == "" || base == "." || base == "/" {
			continue
		}
		if dirInfo, derr := os.Stat(dir); derr != nil || !dirInfo.IsDir() {
			continue
		}
		t, ok := byDir[dir]
		if !ok {
			t = &watchTarget{dir: dir, matchNames: make(map[string]string)}
			byDir[dir] = t
		}
		t.matchNames[base] = resolved
	}

	if len(byDir) == 0 {
		_ = syscall.Close(inotifyFd)
		return nil, errors.New("no watchable paths exist")
	}

	const mask = syscall.IN_MODIFY | syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO | syscall.IN_CREATE
	wdTargets := make(map[int32]*watchTarget, len(byDir))
	for dir, target := range byDir {
		wd, addErr := syscall.InotifyAddWatch(inotifyFd, dir, mask)
		if addErr != nil {
			continue
		}
		wdTargets[int32(wd)] = target
	}
	if len(wdTargets) == 0 {
		_ = syscall.Close(inotifyFd)
		return nil, errors.New("failed to add any inotify watches")
	}

	epollFd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		_ = syscall.Close(inotifyFd)
		return nil, err
	}

	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		_ = syscall.Close(epollFd)
		_ = syscall.Close(inotifyFd)
		return nil, err
	}

	inEv := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(inotifyFd)}
	if err := syscall.EpollCtl(epollFd, syscall.EPOLL_CTL_ADD, inotifyFd, &inEv); err != nil {
		_ = pipeR.Close()
		_ = pipeW.Close()
		_ = syscall.Close(epollFd)
		_ = syscall.Close(inotifyFd)
		return nil, err
	}

	wakeFd := int(pipeR.Fd())
	wakeEv := syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(wakeFd)}
	if err := syscall.EpollCtl(epollFd, syscall.EPOLL_CTL_ADD, wakeFd, &wakeEv); err != nil {
		_ = pipeR.Close()
		_ = pipeW.Close()
		_ = syscall.Close(epollFd)
		_ = syscall.Close(inotifyFd)
		return nil, err
	}

	eventsCh := make(chan string, 32)
	doneCh := make(chan struct{})

	w := &FileWatcher{
		Events:   eventsCh,
		wakeW:    pipeW,
		eventsCh: eventsCh,
		doneCh:   doneCh,
	}

	go w.readLoop(epollFd, inotifyFd, pipeR, wdTargets)
	return w, nil
}

func resolveWatchPath(p string) string {
	if eval, err := filepath.EvalSymlinks(p); err == nil && eval != "" {
		return eval
	}
	// Target file may not exist yet; resolve its parent directory if possible.
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	if evalDir, err := filepath.EvalSymlinks(dir); err == nil && evalDir != "" {
		return filepath.Join(evalDir, base)
	}
	return p
}

func (w *FileWatcher) readLoop(epollFd, inotifyFd int, pipeR *os.File, wdTargets map[int32]*watchTarget) {
	defer close(w.doneCh)
	defer close(w.eventsCh)
	defer func() {
		_ = pipeR.Close()
		_ = syscall.Close(epollFd)
		_ = syscall.Close(inotifyFd)
	}()

	wakeFd := int32(pipeR.Fd())
	var epollEvents [8]syscall.EpollEvent
	buf := make([]byte, 4096)
	const headerSize = syscall.SizeofInotifyEvent

	for {
		nEv, err := syscall.EpollWait(epollFd, epollEvents[:], -1)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return
		}

		for i := 0; i < nEv; i++ {
			if epollEvents[i].Fd == wakeFd {
				return
			}
		}

		// Drain all pending inotify events until EAGAIN.
		for {
			n, rerr := syscall.Read(inotifyFd, buf)
			if rerr != nil {
				if errors.Is(rerr, syscall.EAGAIN) || errors.Is(rerr, syscall.EWOULDBLOCK) {
					break
				}
				if errors.Is(rerr, syscall.EINTR) {
					continue
				}
				return
			}
			if n <= 0 {
				break
			}

			offset := 0
			for offset+headerSize <= n {
				ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
				nameLen := int(ev.Len)
				offset += headerSize
				if offset+nameLen > n {
					break
				}
				var name string
				if nameLen > 0 {
					rawName := buf[offset : offset+nameLen]
					if nul := indexNull(rawName); nul >= 0 {
						name = string(rawName[:nul])
					} else {
						name = string(rawName)
					}
				}
				offset += nameLen

				target, ok := wdTargets[ev.Wd]
				if !ok {
					continue
				}

				var matchedPath string
				if target.watchAll {
					if name != "" {
						matchedPath = filepath.Join(target.dir, name)
					} else {
						matchedPath = target.dir
					}
				} else if p, hit := target.matchNames[name]; hit {
					matchedPath = p
				} else {
					continue
				}

				// Non-blocking send coalesces bursts without stalling the kernel reader.
				select {
				case w.eventsCh <- matchedPath:
				default:
				}
			}
		}
	}
}

func indexNull(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// Close stops the watcher, wakes epoll immediately, closes descriptors, and waits for cleanup.
func (w *FileWatcher) Close() error {
	if w == nil {
		return nil
	}
	var closeErr error
	w.closeOnce.Do(func() {
		if w.wakeW != nil {
			_, _ = w.wakeW.Write([]byte{1})
			closeErr = w.wakeW.Close()
		}
		<-w.doneCh
	})
	return closeErr
}
