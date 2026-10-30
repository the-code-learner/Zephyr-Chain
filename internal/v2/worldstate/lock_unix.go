//go:build linux || darwin

package worldstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// The lock file is deliberately never unlinked: unlinking it lets two
// processes lock different inodes under the same pathname. The kernel
// releases flock automatically when a process exits, including a crash.
func acquireWriterLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, "state.writer.lock")
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, ErrPersistenceBusy
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, ErrPersistenceBusy
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		_ = file.Close()
		return nil, ErrPersistenceBusy
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, ErrPersistenceBusy
	}
	return file, nil
}

func releaseWriterLock(file *os.File) error {
	if file == nil {
		return nil
	}
	// Close releases the per-open-file-description advisory flock.
	return file.Close()
}
